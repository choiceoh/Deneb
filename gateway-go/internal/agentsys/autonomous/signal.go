// signal.go — Proactive event-signal detection (research: docs/research/claw-anything-always-on-assistant.md, finding B).
//
// The Claw-Anything benchmark (arXiv 2605.26086) found proactive assistance ~4x
// harder than reactive (6.7% vs 25.9% pass@1): the agent must FIND the problem in
// a noisy stream, not just answer a stated one. Deneb already has the proactive
// *scaffolding* (the heartbeat task runs on a timer), but its trigger is purely
// time-based — it has no notion of "is anything actually noteworthy right now?".
//
// This file is that missing layer: a pure, dependency-free scorer that turns a
// transport-agnostic snapshot of the user's recent state (mail, calendar,
// deadlines) into a weighted SignalReport. It is deliberately pure so it is fully
// unit-testable without Gmail/Calendar/LLM. Data-source adapters (which fetch the
// snapshot) live at the wiring layer and feed SignalInputs in; the heartbeat task
// uses ShouldEscalate()/Summary() to decide whether — and with what context — to
// run a full agent turn. Keeping detection cheap and explicit also honors the
// "능동적이되 침해적이지 않게" rule (no over-notification): a turn fires only when a
// real signal crosses the threshold.
package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SignalKind classifies a detected proactive signal.
type SignalKind string

const (
	// SignalVIPMailUnanswered: an important/VIP sender's message is unread and
	// has no reply yet — the highest-value proactive nudge.
	SignalVIPMailUnanswered SignalKind = "vip_mail_unanswered"
	// SignalMailStale: an unread, unanswered message has aged past the stale
	// threshold (important or not) — risk of dropping the ball.
	SignalMailStale SignalKind = "mail_stale_unanswered"
	// SignalCalendarConflict: two timed events overlap.
	SignalCalendarConflict SignalKind = "calendar_conflict"
	// SignalEventImminent: a timed event starts within the imminent window
	// (weighted higher when it still needs the user's RSVP).
	SignalEventImminent SignalKind = "event_imminent"
	// SignalDeadlineApproaching: a tracked due item falls within the deadline window.
	SignalDeadlineApproaching SignalKind = "deadline_approaching"
)

// MailSignalInput is a transport-agnostic view of one inbound message. The
// adapter maps Gmail labels/threads onto these plain fields (e.g. Important =
// IMPORTANT label or a wiki-VIP sender; Answered = the user replied in-thread).
type MailSignalInput struct {
	ID         string
	From       string
	Subject    string
	Important  bool
	Unread     bool
	Answered   bool
	ReceivedAt time.Time
}

// EventSignalInput is a transport-agnostic view of one calendar event.
type EventSignalInput struct {
	ID       string
	Summary  string
	Start    time.Time
	End      time.Time
	AllDay   bool
	Canceled bool
	// NeedsResponse is true when the user (self attendee) has not yet RSVP'd
	// (responseStatus == "needsAction").
	NeedsResponse bool
}

// DeadlineSignalInput represents a tracked due item (e.g. a HEARTBEAT.md item or
// cron job carrying a due time).
type DeadlineSignalInput struct {
	Label string
	Due   time.Time
}

// SignalInputs is the snapshot the detector scores. Now anchors all relative
// time comparisons so tests are deterministic.
type SignalInputs struct {
	Now       time.Time
	Mail      []MailSignalInput
	Events    []EventSignalInput
	Deadlines []DeadlineSignalInput
}

// SignalConfig tunes detection thresholds and per-signal weights. Use
// DefaultSignalConfig and override fields as needed.
type SignalConfig struct {
	StaleMailAge        time.Duration // unanswered mail older than this is "stale"
	ImminentEventWindow time.Duration // event starting within this is "imminent"
	DeadlineWindow      time.Duration // deadline within this is "approaching"

	VIPMailWeight   int
	StaleMailWeight int
	ConflictWeight  int
	ImminentWeight  int
	DeadlineWeight  int

	// EscalateThreshold is the minimum total score for ShouldEscalate to be true.
	EscalateThreshold int
	// MaxReasonsPerKind caps how many human-readable reasons of a single kind are
	// listed in Summary (the score still counts every occurrence).
	MaxReasonsPerKind int
}

// DefaultSignalConfig returns conservative defaults tuned to surface only
// genuinely noteworthy state (avoid over-notification). Weights are calibrated so
// a single VIP unanswered mail, one calendar conflict, or one imminent RSVP each
// crosses the threshold on its own, while ordinary stale mail needs to accumulate.
func DefaultSignalConfig() SignalConfig {
	return SignalConfig{
		StaleMailAge:        4 * time.Hour,
		ImminentEventWindow: 30 * time.Minute,
		DeadlineWindow:      24 * time.Hour,
		VIPMailWeight:       50,
		StaleMailWeight:     15,
		ConflictWeight:      50,
		ImminentWeight:      40,
		DeadlineWeight:      30,
		EscalateThreshold:   40,
		MaxReasonsPerKind:   3,
	}
}

// SignalConfigForThreshold returns DefaultSignalConfig with the escalation
// threshold overridden by an operator-supplied value — the single first-class
// "cadence" dial exposed in deneb.json (agents.proactiveEscalateThreshold).
//
// A threshold <= 0 means "unset" and keeps the calibrated default (40), so the
// proactive cadence is byte-identical to before unless the operator deliberately
// tunes it. Lower = more proactive (the heartbeat interrupts on a weaker signal);
// higher = quieter (more must accumulate before it speaks up). Only the
// threshold is operator-tunable; the per-signal weights stay calibrated.
func SignalConfigForThreshold(threshold int) SignalConfig {
	cfg := DefaultSignalConfig()
	if threshold > 0 {
		cfg.EscalateThreshold = threshold
	}
	return cfg
}

// Signal is one detected noteworthy item with a Korean, human-readable reason.
type Signal struct {
	Kind   SignalKind
	Reason string
	Weight int
}

// SignalReport is the detector output: the matched signals, their total score,
// and the threshold used.
type SignalReport struct {
	Signals   []Signal
	Score     int
	Threshold int
}

// ShouldEscalate reports whether the score crosses the threshold (and at least
// one signal fired) — i.e. whether a full proactive turn is warranted.
func (r SignalReport) ShouldEscalate() bool {
	return len(r.Signals) > 0 && r.Score >= r.Threshold
}

// Summary renders a concise Korean block of the detected signals, grouped by
// kind, for prepending to the proactive trigger so the agent prioritizes them.
// Returns "" when there are no signals. maxPerKind caps reasons per kind (<=0
// means no cap).
func (r SignalReport) Summary(maxPerKind int) string {
	if len(r.Signals) == 0 {
		return ""
	}
	order := []SignalKind{
		SignalVIPMailUnanswered, SignalCalendarConflict, SignalEventImminent,
		SignalDeadlineApproaching, SignalMailStale,
	}
	labels := map[SignalKind]string{
		SignalVIPMailUnanswered:   "VIP 미응답 메일",
		SignalCalendarConflict:    "일정 충돌",
		SignalEventImminent:       "임박 일정",
		SignalDeadlineApproaching: "마감 임박",
		SignalMailStale:           "미응답 메일",
	}
	byKind := map[SignalKind][]string{}
	for _, s := range r.Signals {
		byKind[s.Kind] = append(byKind[s.Kind], s.Reason)
	}
	var b strings.Builder
	b.WriteString("[자동 감지 신호 — HEARTBEAT.md 지시보다 먼저 검토하세요]\n")
	for _, kind := range order {
		reasons := byKind[kind]
		if len(reasons) == 0 {
			continue
		}
		extra := 0
		if maxPerKind > 0 && len(reasons) > maxPerKind {
			extra = len(reasons) - maxPerKind
			reasons = reasons[:maxPerKind]
		}
		for _, reason := range reasons {
			fmt.Fprintf(&b, "- %s: %s\n", labels[kind], reason)
		}
		if extra > 0 {
			fmt.Fprintf(&b, "- %s: 외 %d건\n", labels[kind], extra)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// DetectSignals scores a snapshot into a SignalReport. It is pure: same inputs +
// config always yield the same report. Detection rules:
//
//   - VIP/important mail that is unread and unanswered  → VIPMailWeight each
//   - any other unread, unanswered mail older than StaleMailAge → StaleMailWeight each
//   - any two timed (non-all-day, non-canceled) events that overlap → ConflictWeight per pair
//   - a timed, non-canceled event starting within ImminentEventWindow → ImminentWeight
//     (NeedsResponse events still count when imminent; RSVP urgency is noted in the reason)
//   - a deadline due within DeadlineWindow (and not past) → DeadlineWeight each
func DetectSignals(in SignalInputs, cfg SignalConfig) SignalReport {
	now := in.Now
	rep := SignalReport{Threshold: cfg.EscalateThreshold}

	add := func(kind SignalKind, weight int, reason string) {
		rep.Signals = append(rep.Signals, Signal{Kind: kind, Reason: reason, Weight: weight})
		rep.Score += weight
	}

	// --- Mail: VIP-unanswered (high) vs stale-unanswered (accumulating) ---
	for _, m := range in.Mail {
		if m.Answered || !m.Unread {
			continue
		}
		who := mailWho(m)
		if m.Important {
			add(SignalVIPMailUnanswered, cfg.VIPMailWeight, who)
			continue
		}
		if cfg.StaleMailAge > 0 && !m.ReceivedAt.IsZero() &&
			now.Sub(m.ReceivedAt) >= cfg.StaleMailAge {
			age := now.Sub(m.ReceivedAt).Round(time.Hour)
			add(SignalMailStale, cfg.StaleMailWeight, fmt.Sprintf("%s (%s 경과)", who, humanizeDuration(age)))
		}
	}

	// --- Calendar conflicts: overlapping timed events ---
	timed := make([]EventSignalInput, 0, len(in.Events))
	for _, e := range in.Events {
		if e.Canceled || e.AllDay || e.Start.IsZero() || e.End.IsZero() {
			continue
		}
		timed = append(timed, e)
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].Start.Before(timed[j].Start) })
	for i := range timed {
		for j := i + 1; j < len(timed); j++ {
			// Sorted by start; once j starts at/after i ends, no later j overlaps i.
			if !timed[j].Start.Before(timed[i].End) {
				break
			}
			add(SignalCalendarConflict, cfg.ConflictWeight,
				fmt.Sprintf("%q ↔ %q", eventTitle(timed[i]), eventTitle(timed[j])))
		}
	}

	// --- Imminent events ---
	if cfg.ImminentEventWindow > 0 {
		for _, e := range in.Events {
			if e.Canceled || e.AllDay || e.Start.IsZero() {
				continue
			}
			until := e.Start.Sub(now)
			if until < 0 || until > cfg.ImminentEventWindow {
				continue
			}
			reason := fmt.Sprintf("%q %s 후 시작", eventTitle(e), humanizeDuration(until.Round(time.Minute)))
			if e.NeedsResponse {
				reason += " (미회신)"
			}
			add(SignalEventImminent, cfg.ImminentWeight, reason)
		}
	}

	// --- Deadlines ---
	if cfg.DeadlineWindow > 0 {
		for _, d := range in.Deadlines {
			if d.Due.IsZero() {
				continue
			}
			until := d.Due.Sub(now)
			if until < 0 || until > cfg.DeadlineWindow {
				continue
			}
			add(SignalDeadlineApproaching, cfg.DeadlineWeight,
				fmt.Sprintf("%s (%s 남음)", d.Label, humanizeDuration(until.Round(time.Minute))))
		}
	}

	return rep
}

func mailWho(m MailSignalInput) string {
	from := strings.TrimSpace(m.From)
	subj := strings.TrimSpace(m.Subject)
	switch {
	case from != "" && subj != "":
		return fmt.Sprintf("%s %q", from, subj)
	case subj != "":
		return fmt.Sprintf("%q", subj)
	case from != "":
		return from
	default:
		return "(제목 없음)"
	}
}

func eventTitle(e EventSignalInput) string {
	if s := strings.TrimSpace(e.Summary); s != "" {
		return s
	}
	return "(제목 없는 일정)"
}

// humanizeDuration renders a positive duration in Korean (분/시간/일), coarse by
// design — proactive summaries don't need second precision.
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Hour:
		m := int(d.Minutes())
		if m <= 0 {
			m = 1
		}
		return fmt.Sprintf("%d분", m)
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일", int(d.Hours()/24))
	}
}
