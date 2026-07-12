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
package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
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

// HarvestStateFile is the durable post-meeting dedup state filename.
const HarvestStateFile = harvestStateFile

type meetingHarvestState struct {
	Version  int              `json:"version"`
	Asked    map[string]int64 `json:"asked"`              // harvestKey → asked-at unix millis
	Recorded map[string]int64 `json:"recorded,omitempty"` // harvestKey → attendance-logged unix millis
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
	// recordAttendance, if set, silently logs that the operator attended a
	// matched meeting — regardless of the ask cap, so the meeting fact is
	// remembered even when no follow-up is asked or answered. nil disables it.
	recordAttendance func(target string, ev calendar.Event)

	mu    sync.Mutex
	state meetingHarvestState
}

// SetAttendanceRecorder wires the silent meeting-attendance logger (see the
// recordAttendance field). Called once at registration.
func (s *meetingHarvestService) SetAttendanceRecorder(fn func(target string, ev calendar.Event)) {
	if s != nil {
		s.recordAttendance = fn
	}
}

// HarvestService asks a bounded follow-up after work-linked meetings.
type HarvestService = meetingHarvestService

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

// NewHarvestService constructs the post-meeting outcome capture worker.
func NewHarvestService(
	deliver func(text string) (bool, error),
	resolve func() (CalendarClient, error),
	matchTarget func(text string) string,
	statePath string,
	logger *slog.Logger,
) *HarvestService {
	return newMeetingHarvestService(deliver, resolve, matchTarget, statePath, logger)
}

// Start launches the periodic meeting-harvest loop until ctx is cancelled.
func (s *meetingHarvestService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	safego.GoWithSlog(s.logger, "meeting-harvest", func() {
		ticker := time.NewTicker(harvestPollInterval)
		defer ticker.Stop()
		// Immediate first pass (briefing-service pattern): waiting for the
		// first ticker fire would let a restart silently eat the tail of a
		// meeting's ask window near the harvestMaxAfterEnd boundary.
		s.tick(ctx)
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
	// Window note: long meetings that STARTED before this window are still
	// fetched — Google's timeMin bounds the event's END time, and the local
	// merge uses overlap (Start < to && End > from; calendar_briefing.go) —
	// so a 5-hour workshop that ended 10 minutes ago is considered.
	events, err := client.ListUpcoming(fetchCtx, now.Add(-harvestMaxAfterEnd), now, calendarMaxResults)
	if err != nil {
		s.logger.Warn("meeting harvest: calendar fetch failed", "error", err)
		return
	}

	// Attendance: silently log every matched, ended meeting to its project —
	// independent of the ask cap, so the meeting is remembered even when no
	// follow-up is asked/answered. Deduped by event key, persisted.
	s.recordAttendances(now, events)

	cands := decideHarvests(now, events, s.alreadyAsked, s.askedToday(now), s.matchTarget, s.displayLoc)
	for _, c := range cands {
		body := formatHarvestAsk(c.Event, c.Target, s.displayLoc)
		delivered, derr := s.deliver(body)
		if derr != nil {
			s.logger.Warn("meeting harvest: push failed", "summary", c.Event.Summary, "error", derr)
			continue
		}
		if !delivered {
			// No connected client (app closed) is a NORMAL state, and the
			// candidate retries every poll until its 3h window closes — Warn
			// here would spam ~36 lines per meeting. Debug, retry silently.
			s.logger.Debug("meeting harvest: no client to deliver to; will retry", "summary", c.Event.Summary)
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

// recordAttendances logs attendance for every matched, ended meeting not yet
// recorded — no ask cap, no quiet-hours gate (it never notifies the user).
func (s *meetingHarvestService) recordAttendances(now time.Time, events []calendar.Event) {
	if s.recordAttendance == nil {
		return
	}
	for _, ev := range events {
		target := matchEndedMeeting(now, ev, s.matchTarget)
		if target == "" {
			continue
		}
		key := harvestKey(ev)
		s.mu.Lock()
		if s.state.Recorded == nil {
			s.state.Recorded = map[string]int64{}
		}
		if _, done := s.state.Recorded[key]; done {
			s.mu.Unlock()
			continue
		}
		s.state.Recorded[key] = now.UnixMilli()
		s.mu.Unlock()

		s.recordAttendance(target, ev)
		s.saveState()
	}
}

// matchEndedMeeting returns the work target for an event that is a genuine,
// recently-ended, in-window human meeting, or "" otherwise. Shared by the ask
// flow (decideHarvests) and the attendance recorder so their notions of "a
// meeting the operator attended" stay identical.
func matchEndedMeeting(now time.Time, ev calendar.Event, matchTarget func(string) string) string {
	if ev.AllDay || ev.Status == "cancelled" || ev.End.IsZero() {
		return ""
	}
	age := now.Sub(ev.End)
	if age < harvestMinAfterEnd || age > harvestMaxAfterEnd {
		return ""
	}
	if selfDeclined(ev.Attendees) {
		// A declined invite can stay on the feed; the operator wasn't there.
		return ""
	}
	if !isMeetingShaped(ev) {
		// A project-linked task block ("발주"/"서류 제출") is not a meeting.
		return ""
	}
	// Attendee/organizer names join the match text: Google events often carry
	// the counterparty only in the guest list — or, for externally organized
	// invites, only in the organizer — not the title. Emails contribute their
	// LOCAL PART only: domain fragments ("co", "kr", "gmail") would otherwise
	// feed the loose unique-token matcher and bind a personal invite to an
	// unrelated project.
	text := ev.Summary + " " + ev.Description
	for _, a := range ev.Attendees {
		text += " " + a.DisplayName + " " + emailLocalPart(a.Email)
	}
	if ev.Organizer.DisplayName != "" || ev.Organizer.Email != "" {
		text += " " + ev.Organizer.DisplayName + " " + emailLocalPart(ev.Organizer.Email)
	}
	return matchTarget(strings.TrimSpace(text))
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
		if alreadyAsked != nil && alreadyAsked(harvestKey(ev)) {
			continue
		}
		target := matchEndedMeeting(now, ev, matchTarget)
		if target == "" {
			continue // not a work-linked, recently-ended meeting
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

// meetingWords are Korean calendar-title tokens that signal an actual human
// encounter. Calibrated against the operator's REAL calendar (17-event sample,
// 2026-07-05): 회식·견학 etc. appear verbatim there. Generic task verbs (발주,
// 제출, 송금, 마감…) are deliberately absent so task blocks never trigger.
var meetingWords = []string{
	"미팅", "회의", "면담", "방문", "상담", "협의", "만남", "통화", "전화",
	"점심", "저녁", "식사", "회식", "킥오프", "발표", "인터뷰", "출장", "외근",
	"견학", "워크숍", "워크샵", "세미나", "컨퍼런스", "박람회", "전시회", "답사",
}

// jobTitleWords: the DOMINANT pattern in the operator's real calendar is
// "<회사> <이름> <직함>" with no meeting word at all ("한화 유성민 팀장",
// "프라임에너지 대표 미팅", "singsun 동사장"). A job title in the text is
// strong evidence of meeting a person. False-positive-ish hits ("집 이사")
// are neutralized by the second lock: a work target must ALSO match before
// anything fires.
var jobTitleWords = []string{
	"대표", "사장", "부사장", "회장", "전무", "상무", "이사", "본부장", "실장",
	"소장", "부장", "차장", "과장", "팀장", "대리", "주임", "위원", "교수",
	"총괄", "매니저",
}

// isMeetingShaped reports whether the event looks like the operator actually
// met (or called) someone: structural evidence first — external attendees, an
// attached conference link, a physical location — then a meeting-word or a
// job-title word in the title/description. Hand-added local events (the
// operator's main calendar) carry no attendee metadata, so the word lists are
// what keeps those alive.
func isMeetingShaped(ev calendar.Event) bool {
	if len(externalAttendees(ev.Attendees, 1)) > 0 {
		return true
	}
	if ev.Conference != nil {
		return true
	}
	if strings.TrimSpace(ev.Location) != "" {
		return true
	}
	text := ev.Summary + " " + ev.Description
	for _, w := range meetingWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	for _, w := range jobTitleWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// looseUniqueNameMatch recovers targets the exact matchers miss: real calendar
// titles are terse ("비금도 … 견학", "JA 이용원 상무") while wiki names are long
// compound forms ("비금도-154kv-케이블-및-액세서리-(ztt)", "JA Solar"), so
// containment-of-full-name never fires. Here an event TOKEN matching INSIDE
// exactly ONE known name (projects and ledgers judged JOINTLY, so a token like
// "lg" that spans both lists stays ambiguous) resolves that name; ambiguous
// tokens ("당진" spans three projects) resolve to nothing rather than guessing.
func looseUniqueNameMatch(text string, names []string) string {
	toks := harvestTokens(text)
	if len(toks) == 0 {
		return ""
	}
	// Match against the ENTITY part of each name only — its first two
	// segments. Long descriptive slugs embed generic nouns ("강진-신다산-epc-
	// 계약서-법무검토…"), and a title token like 계약서 latching onto that tail
	// was a real fidelity-drill false match. The entity lives up front.
	keys := make([]string, len(names))
	for i, n := range names {
		keys[i] = harvestNorm(strings.Join(firstFields(n, 2), ""))
	}
	for _, tok := range toks {
		hit := ""
		count := 0
		for i := range names {
			if strings.Contains(keys[i], tok) {
				count++
				hit = names[i]
			}
		}
		if count == 1 {
			return hit
		}
	}
	return ""
}

// harvestKnownNames flattens the project + ledger name sets for loose matching.
func harvestKnownNames(projects []wiki.ProjectRef, ledgers []wiki.CounterpartyRef) []string {
	out := make([]string, 0, len(projects)+len(ledgers))
	for _, p := range projects {
		out = append(out, p.Name)
	}
	for _, c := range ledgers {
		out = append(out, c.Name)
	}
	return out
}

// KnownNames returns the project and counterparty names considered by the
// deterministic meeting-title matcher.
func KnownNames(projects []wiki.ProjectRef, ledgers []wiki.CounterpartyRef) []string {
	return harvestKnownNames(projects, ledgers)
}

// LooseUniqueNameMatch returns the one known name uniquely identified by the
// title's meaningful tokens, or an empty string when the match is ambiguous.
func LooseUniqueNameMatch(text string, names []string) string {
	return looseUniqueNameMatch(text, names)
}

// harvestTokens splits free text into normalized candidate tokens. Guards
// learned from the real-17 fidelity drill:
//   - pure-digit tokens never match ("6/25" split into "25" latched onto a
//     project named "…(2026-06-25)" — a date is not an identity);
//   - Hangul tokens need ≥3 runes (2-rune ones are mostly function words —
//     바로, 관련 — that collide with company names);
//   - ASCII abbreviations keep ≥2 ("JA", "LG") — Korean business shorthand,
//     with the joint-uniqueness rule absorbing the collisions.
func harvestTokens(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out []string
	for _, f := range fields {
		tok := harvestNorm(f)
		n := utf8.RuneCountInString(tok)
		if n < 2 {
			continue
		}
		digitsOnly, ascii := true, true
		for _, r := range tok {
			if r < '0' || r > '9' {
				digitsOnly = false
			}
			if r > unicode.MaxASCII {
				ascii = false
			}
		}
		if digitsOnly {
			continue
		}
		if !ascii && n < 3 {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// firstFields returns up to max leading letter/digit runs of s.
func firstFields(s string, max int) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(fields) > max {
		fields = fields[:max]
	}
	return fields
}

// harvestNorm lowercases and strips non-letter/digit runes — the same shape the
// wiki's name keys use, kept local because this normalizes EVENT tokens, not
// wiki paths (no layout-rule duplication).
func harvestNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
// Korean-first, two short lines. The body MUST end with a question mark:
// the work-feed relay marks a card answerable (free-text reply field) only
// when the trimmed body ends with ?/？ (proactive_relay.go isQuestion →
// endsWithQuestionMark) — a statement-final body would deliver the nudge
// without the reply path this feature exists for.
func formatHarvestAsk(ev calendar.Event, target string, loc *time.Location) string {
	title := strings.TrimSpace(ev.Summary)
	if title == "" {
		title = "일정"
	}
	span := ev.Start.In(loc).Format("15:04") + "~" + ev.End.In(loc).Format("15:04")
	var b strings.Builder
	fmt.Fprintf(&b, "🗒 %s (%s) 끝나셨죠?", title, span)
	fmt.Fprintf(&b, "\n%s 기록에 남겨둘게요 — 결과나 결정된 것 있으면 한 줄로 알려주시겠어요?", target)
	return b.String()
}

// selfDeclined reports whether the authenticated user's own attendee entry
// declined the invitation.
func selfDeclined(attendees []calendar.Attendee) bool {
	for _, a := range attendees {
		if a.Self && a.ResponseStatus == "declined" {
			return true
		}
	}
	return false
}

// emailLocalPart strips the domain: "kim@partner.co.kr" → "kim".
func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i >= 0 {
		return email[:i]
	}
	return email
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
	for k, ms := range s.state.Recorded {
		if ms < cutoff {
			delete(s.state.Recorded, k)
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
