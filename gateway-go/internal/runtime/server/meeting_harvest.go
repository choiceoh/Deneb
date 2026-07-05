// meeting_harvest.go — post-meeting outcome capture ("○○ 어떻게 되셨어요?").
//
// The knowledge flywheel is mail-heavy: the other half of every negotiation
// happens in meetings and calls, and those outcomes evaporate unless the
// operator volunteers them. This service closes that loop from the calendar
// side with ZERO manual capture: when a project- or counterparty-linked event
// ends, it pushes one short follow-up question into the client:main transcript
// (same proactive relay as the D-15 briefing). The operator's reply is then an
// ordinary main-session turn — the question sits right above it naming the
// project, so the agent's standing wiki instruction (사건·회의 소식은 해당
// 프로젝트 로그.md에 append) files the answer into the flywheel.
//
// 개입 기준 (over-notification 금지):
//   - project/counterparty-matched events ONLY — personal events never trigger
//     (deterministic wiki matchers, no LLM);
//   - ≥10 min after the event ends (let the user leave the room);
//   - at most 2 asks per day, 08–21 KST;
//   - dedup persisted across restarts (auto-deploy restarts the gateway on
//     every merge batch; an in-memory map would re-ask the same meeting).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	harvestPollInterval = 5 * time.Minute
	// harvestMinAfterEnd: don't ask while the user is plausibly still in the
	// room; harvestMaxAfterEnd: past this, the moment is gone — asking about a
	// meeting from this morning at dinnertime reads as nagging.
	harvestMinAfterEnd = 10 * time.Minute
	harvestMaxAfterEnd = 3 * time.Hour
	harvestDailyCap    = 2
	harvestActiveFrom  = 8  // KST hour, inclusive
	harvestActiveUntil = 21 // KST hour, exclusive
	// harvestStateFile persists asked-event keys under the state dir.
	harvestStateFile = "meeting-harvest-state.json"
	// harvestStateRetention prunes asked entries; well past harvestMaxAfterEnd
	// so a pruned key can never re-trigger.
	harvestStateRetention = 7 * 24 * time.Hour
)

type meetingHarvestState struct {
	Version int              `json:"version"`
	Asked   map[string]int64 `json:"asked"` // harvestKey → asked-at unix millis
}

// meetingHarvestService polls the calendar for recently-ended, work-linked
// events and pushes one follow-up question per event. Mirrors
// calendarBriefingService's lifecycle (nil-safe start, safego goroutine,
// ShutdownCtx-bound).
type meetingHarvestService struct {
	deliver func(text string) (bool, error)
	resolve func() (briefingCalendarClient, error)
	// matchTarget maps event text to a known project/counterparty name, or ""
	// when the event is not work-linked (→ never harvested).
	matchTarget func(text string) string
	logger      *slog.Logger
	statePath   string // "" → in-memory only
	displayLoc  *time.Location

	mu    sync.Mutex
	state meetingHarvestState
}

func newMeetingHarvestService(
	deliver func(text string) (bool, error),
	resolve func() (briefingCalendarClient, error),
	matchTarget func(text string) string,
	statePath string,
	logger *slog.Logger,
) *meetingHarvestService {
	if deliver == nil || resolve == nil || matchTarget == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.FixedZone("KST", kstFallbackOffset)
	}
	s := &meetingHarvestService{
		deliver:     deliver,
		resolve:     resolve,
		matchTarget: matchTarget,
		logger:      logger,
		statePath:   statePath,
		displayLoc:  loc,
		state:       meetingHarvestState{Version: 1, Asked: map[string]int64{}},
	}
	s.loadState()
	return s
}

func (s *meetingHarvestService) start(ctx context.Context) {
	if s == nil {
		return
	}
	safego.GoWithSlog(s.logger, "meeting-harvest", func() {
		ticker := time.NewTicker(harvestPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logger.Debug("meeting harvest service stopped")
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	})
}

func (s *meetingHarvestService) tick(ctx context.Context) {
	client, err := s.resolve()
	if err != nil {
		return // calendar not configured — quiet skip (briefing logs this path)
	}
	now := time.Now()
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	events, err := client.ListUpcoming(fetchCtx, now.Add(-harvestMaxAfterEnd), now, calendarMaxResults)
	if err != nil {
		s.logger.Warn("meeting harvest: calendar fetch failed", "error", err)
		return
	}

	cands := decideHarvests(now, events, s.alreadyAsked, s.askedToday(now), s.matchTarget, s.displayLoc)
	for _, c := range cands {
		body := formatHarvestAsk(c.Event, c.Target, s.displayLoc)
		delivered, derr := s.deliver(body)
		if derr != nil || !delivered {
			s.logger.Warn("meeting harvest: push failed", "summary", c.Event.Summary, "error", derr)
			continue
		}
		s.markAsked(harvestKey(c.Event), now)
		s.logger.Info("meeting harvest: follow-up asked",
			"summary", c.Event.Summary, "target", c.Target)
	}
	s.pruneState(now)
}

// harvestCandidate pairs an ended event with its matched work target.
type harvestCandidate struct {
	Event  calendar.Event
	Target string
}

// decideHarvests is the pure selection function (production and tests share
// it): ended work-linked events inside the ask window, oldest first, capped by
// the remaining daily budget. askedToday is how many asks already went out
// today; quiet hours are evaluated in loc.
func decideHarvests(
	now time.Time,
	events []calendar.Event,
	alreadyAsked func(string) bool,
	askedToday int,
	matchTarget func(string) string,
	loc *time.Location,
) []harvestCandidate {
	if loc == nil {
		loc = time.Local
	}
	if h := now.In(loc).Hour(); h < harvestActiveFrom || h >= harvestActiveUntil {
		return nil
	}
	budget := harvestDailyCap - askedToday
	if budget <= 0 {
		return nil
	}
	var out []harvestCandidate
	for _, ev := range events {
		if ev.AllDay || ev.Status == "cancelled" || ev.End.IsZero() {
			continue
		}
		age := now.Sub(ev.End)
		if age < harvestMinAfterEnd || age > harvestMaxAfterEnd {
			continue
		}
		if alreadyAsked != nil && alreadyAsked(harvestKey(ev)) {
			continue
		}
		target := matchTarget(strings.TrimSpace(ev.Summary + " " + ev.Description))
		if target == "" {
			continue // not work-linked — personal events are never harvested
		}
		out = append(out, harvestCandidate{Event: ev, Target: target})
	}
	// Oldest end first, so the meeting closest to falling out of the window
	// gets the budget.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Event.End.Before(out[j-1].Event.End); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > budget {
		out = out[:budget]
	}
	return out
}

// harvestKey identifies one occurrence: same event ID rescheduled to a new end
// gets a fresh ask.
func harvestKey(ev calendar.Event) string {
	id := ev.ID
	if id == "" {
		id = ev.Summary
	}
	return id + "|" + fmt.Sprintf("%d", ev.End.Unix())
}

// formatHarvestAsk renders the deterministic follow-up question. Plain text,
// Korean-first, two short lines — a question, not a report.
func formatHarvestAsk(ev calendar.Event, target string, loc *time.Location) string {
	title := strings.TrimSpace(ev.Summary)
	if title == "" {
		title = "일정"
	}
	span := ev.Start.In(loc).Format("15:04") + "~" + ev.End.In(loc).Format("15:04")
	var b strings.Builder
	fmt.Fprintf(&b, "🗒 %s (%s) 끝나셨죠?", title, span)
	fmt.Fprintf(&b, "\n결과나 결정된 것 있으면 한 줄로 알려주세요 — %s 기록에 남겨둘게요.", target)
	return b.String()
}

// --- state (asked-key dedup, restart-safe) ---------------------------------

func (s *meetingHarvestService) alreadyAsked(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.state.Asked[key]
	return ok
}

// askedToday counts asks whose timestamp falls on today's date in displayLoc.
func (s *meetingHarvestService) askedToday(now time.Time) int {
	today := now.In(s.displayLoc).Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, ms := range s.state.Asked {
		if time.UnixMilli(ms).In(s.displayLoc).Format("2006-01-02") == today {
			n++
		}
	}
	return n
}

func (s *meetingHarvestService) markAsked(key string, now time.Time) {
	s.mu.Lock()
	s.state.Asked[key] = now.UnixMilli()
	s.mu.Unlock()
	s.saveState()
}

func (s *meetingHarvestService) pruneState(now time.Time) {
	cutoff := now.Add(-harvestStateRetention).UnixMilli()
	s.mu.Lock()
	changed := false
	for k, ms := range s.state.Asked {
		if ms < cutoff {
			delete(s.state.Asked, k)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.saveState()
	}
}

func (s *meetingHarvestService) loadState() {
	if s.statePath == "" {
		return
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return // missing → fresh state
	}
	var st meetingHarvestState
	if err := json.Unmarshal(data, &st); err != nil {
		s.logger.Warn("meeting harvest: corrupt state, starting fresh", "error", err)
		return
	}
	if st.Asked == nil {
		st.Asked = map[string]int64{}
	}
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *meetingHarvestService) saveState() {
	if s.statePath == "" {
		return
	}
	s.mu.Lock()
	data, err := json.MarshalIndent(s.state, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(s.statePath, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		s.logger.Warn("meeting harvest: failed to persist state", "error", err)
	}
}
