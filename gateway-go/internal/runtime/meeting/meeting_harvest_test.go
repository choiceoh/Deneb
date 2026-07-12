package meeting

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

var harvestKST = time.FixedZone("KST", 9*60*60)

func harvestTestMatcher(text string) string {
	if strings.Contains(text, "영산고") {
		return "영산고"
	}
	return ""
}

// TestRecordAttendances pins the silent attendance pass: it records every
// matched, ended meeting once — no ask cap, deduped across ticks — and never
// touches personal/unmatched events.
func TestRecordAttendances(t *testing.T) {
	now := time.Date(2026, 7, 6, 14, 0, 0, 0, harvestKST)
	mk := func(id, summary string, endedAgo time.Duration) calendar.Event {
		end := now.Add(-endedAgo)
		return calendar.Event{
			ID: id, Summary: summary,
			Start: end.Add(-time.Hour), End: end, Status: "confirmed",
		}
	}
	m1 := mk("m1", "영산고 발주 미팅", 30*time.Minute)
	m2 := mk("m2", "영산고 시공 미팅", 90*time.Minute)
	personal := mk("p", "치과 예약", 30*time.Minute)
	events := []calendar.Event{m1, m2, personal}

	var recorded []string
	s := &meetingHarvestService{
		matchTarget: harvestTestMatcher,
		displayLoc:  harvestKST,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		state:       meetingHarvestState{Version: 1, Asked: map[string]int64{}},
		recordAttendance: func(ev calendar.Event) bool {
			recorded = append(recorded, ev.ID)
			return true
		},
	}

	s.recordAttendances(now, events)
	if len(recorded) != 2 {
		t.Fatalf("recorded %v, want 2 matched meetings (personal excluded)", recorded)
	}

	// A second tick records nothing new (deduped by event key).
	before := len(recorded)
	s.recordAttendances(now, events)
	if len(recorded) != before {
		t.Errorf("re-recorded on second tick: %v", recorded)
	}

	// No recorder wired → no-op, no panic.
	(&meetingHarvestService{matchTarget: harvestTestMatcher}).recordAttendances(now, events)
}

// TestRecordAttendancesRecordBeforeMark pins the review fix: an event is marked
// Recorded only AFTER the recorder returns, so a recorder that fails does not
// permanently suppress the meeting. The recorder observes its own key as NOT
// yet marked at call time.
func TestRecordAttendancesRecordBeforeMark(t *testing.T) {
	now := time.Date(2026, 7, 6, 14, 0, 0, 0, harvestKST)
	end := now.Add(-30 * time.Minute)
	ev := calendar.Event{
		ID: "m1", Summary: "영산고 발주 미팅",
		Start: end.Add(-time.Hour), End: end, Status: "confirmed",
	}
	key := harvestKey(ev)

	var markedAtCallTime bool
	s := &meetingHarvestService{
		matchTarget: harvestTestMatcher,
		displayLoc:  harvestKST,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		state:       meetingHarvestState{Version: 1, Asked: map[string]int64{}},
	}
	s.recordAttendance = func(e calendar.Event) bool {
		s.mu.Lock()
		_, markedAtCallTime = s.state.Recorded[key]
		s.mu.Unlock()
		return true
	}

	s.recordAttendances(now, []calendar.Event{ev})
	if markedAtCallTime {
		t.Error("event was marked Recorded before the recorder ran — a failed record would be lost")
	}
	if _, done := s.state.Recorded[key]; !done {
		t.Error("event must be marked Recorded after the recorder returns")
	}
}

// TestRecordAttendancesRetryOnFailure pins the QK91E semantics: a recorder that
// reports a transient failure (false) leaves the event UNMARKED so the next
// poll retries, and a later success marks it.
func TestRecordAttendancesRetryOnFailure(t *testing.T) {
	now := time.Date(2026, 7, 6, 14, 0, 0, 0, harvestKST)
	end := now.Add(-30 * time.Minute)
	ev := calendar.Event{
		ID: "m1", Summary: "영산고 발주 미팅",
		Start: end.Add(-time.Hour), End: end, Status: "confirmed",
	}
	key := harvestKey(ev)

	fail := true
	calls := 0
	s := &meetingHarvestService{
		matchTarget: harvestTestMatcher,
		displayLoc:  harvestKST,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		state:       meetingHarvestState{Version: 1, Asked: map[string]int64{}},
		recordAttendance: func(e calendar.Event) bool {
			calls++
			return !fail
		},
	}

	s.recordAttendances(now, []calendar.Event{ev}) // recorder fails
	if _, done := s.state.Recorded[key]; done {
		t.Error("a failed record must not be marked Recorded")
	}
	fail = false
	s.recordAttendances(now, []calendar.Event{ev}) // retry succeeds
	if _, done := s.state.Recorded[key]; !done {
		t.Error("a successful retry must mark the event Recorded")
	}
	if calls != 2 {
		t.Errorf("recorder calls = %d, want 2 (fail then retry)", calls)
	}
}

func TestDecideHarvests(t *testing.T) {
	now := time.Date(2026, 7, 6, 14, 0, 0, 0, harvestKST)
	mk := func(id, summary string, endedAgo time.Duration) calendar.Event {
		end := now.Add(-endedAgo)
		return calendar.Event{
			ID: id, Summary: summary,
			Start: end.Add(-time.Hour), End: end, Status: "confirmed",
		}
	}
	evA := mk("a", "영산고 발주 미팅", 30*time.Minute)
	evFresh := mk("b", "영산고 협의", 5*time.Minute)      // too fresh (still in the room)
	evOld := mk("c", "영산고 킥오프", 4*time.Hour)         // window expired
	evPersonal := mk("f", "치과 예약", 40*time.Minute)   // not work-linked
	evAsked := mk("g", "영산고 결재 미팅", 40*time.Minute)  // already asked
	evOlder := mk("h", "영산고 시공사 미팅", 90*time.Minute) // candidate, older end
	evAllDay := calendar.Event{ID: "d", Summary: "영산고 종일", AllDay: true, End: now.Add(-time.Hour)}
	evCancelled := mk("e", "영산고 취소건", 30*time.Minute)
	evCancelled.Status = "cancelled"

	// Task blocks are not meetings: project-linked but no attendees, no
	// location, no meeting word → never asked about (operator feedback:
	// 사람을 실제로 만나야 미팅이지, "발주"는 할 일이다).
	evTask := mk("t", "영산고 발주", 30*time.Minute)
	// Structural meeting evidence (external attendee) qualifies even without
	// a meeting word in the title.
	evAttendee := mk("i", "영산고 발주 건", 20*time.Minute)
	evAttendee.Attendees = []calendar.Attendee{{Email: "kim@partner.co.kr", DisplayName: "김부장"}}

	asked := func(key string) bool { return key == harvestKey(evAsked) }
	events := []calendar.Event{evA, evFresh, evOld, evAllDay, evCancelled, evPersonal, evAsked, evOlder, evTask, evAttendee}

	got := decideHarvests(now, events, asked, 0, harvestTestMatcher, harvestKST)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want 2 (h, a — cap 2; i is 3rd-oldest)", got)
	}
	// Oldest end first: h (90min ago) before a (30min ago); the task block is
	// excluded as a non-meeting even though it is project-linked.
	if got[0].Event.ID != "h" || got[1].Event.ID != "a" {
		t.Errorf("order = %s,%s want h,a", got[0].Event.ID, got[1].Event.ID)
	}
	// With a bigger budget the attendee-shaped event joins; the task never does.
	all := decideHarvests(now, events, asked, -1, harvestTestMatcher, harvestKST)
	ids := map[string]bool{}
	for _, c := range all {
		ids[c.Event.ID] = true
	}
	if !ids["i"] || ids["t"] {
		t.Errorf("attendee event must qualify, task block must not: %v", ids)
	}
	if got[0].Target != "영산고" {
		t.Errorf("target = %q", got[0].Target)
	}

	// Daily budget: one ask already out today → only the oldest survives.
	got = decideHarvests(now, events, asked, 1, harvestTestMatcher, harvestKST)
	if len(got) != 1 || got[0].Event.ID != "h" {
		t.Errorf("budget-1 candidates = %+v, want [h]", got)
	}
	// Cap exhausted → nothing.
	if got = decideHarvests(now, events, asked, harvestDailyCap, harvestTestMatcher, harvestKST); len(got) != 0 {
		t.Errorf("cap exhausted should yield none, got %+v", got)
	}

	// Quiet hours (22:00 KST) → nothing, regardless of candidates.
	late := time.Date(2026, 7, 6, 22, 0, 0, 0, harvestKST)
	if got = decideHarvests(late, events, asked, 0, harvestTestMatcher, harvestKST); len(got) != 0 {
		t.Errorf("quiet hours should yield none, got %+v", got)
	}
}

func TestFormatHarvestAsk(t *testing.T) {
	end := time.Date(2026, 7, 6, 15, 0, 0, 0, harvestKST)
	ev := calendar.Event{Summary: "영산고 발주 미팅", Start: end.Add(-time.Hour), End: end}
	got := formatHarvestAsk(ev, "영산고", harvestKST)
	for _, want := range []string{"영산고 발주 미팅", "14:00~15:00", "영산고 기록"} {
		if !strings.Contains(got, want) {
			t.Errorf("ask missing %q:\n%s", want, got)
		}
	}
	// The relay marks a card answerable only when the trimmed body ends with a
	// question mark (proactive_relay.go endsWithQuestionMark) — without this
	// suffix the feed shows the nudge but no reply field.
	if !strings.HasSuffix(strings.TrimSpace(got), "?") {
		t.Errorf("ask must end with a question mark to render the answer field:\n%s", got)
	}
}

// TestDecideHarvests_ReviewFindings pins the #3114 review fixes: declined
// invites stay silent, organizer-only counterparties match, and attendee
// email DOMAIN fragments never reach the matcher (local part only).
func TestDecideHarvests_ReviewFindings(t *testing.T) {
	now := time.Date(2026, 7, 6, 14, 0, 0, 0, harvestKST)
	end := now.Add(-30 * time.Minute)

	// Declined self-invite: project-linked and meeting-shaped, but the
	// operator said no — never ask "끝나셨죠?" about a skipped meeting.
	declined := calendar.Event{
		ID: "d1", Summary: "영산고 발주 미팅", Start: end.Add(-time.Hour), End: end, Status: "confirmed",
		Attendees: []calendar.Attendee{
			{Email: "me@deneb.ai", Self: true, ResponseStatus: "declined"},
			{Email: "kim@partner.co.kr", DisplayName: "김부장"},
		},
	}
	if got := decideHarvests(now, []calendar.Event{declined}, nil, 0, harvestTestMatcher, harvestKST); len(got) != 0 {
		t.Errorf("declined invite must not be harvested, got %+v", got)
	}

	// Organizer-only counterparty: externally organized invite where the
	// project name lives only on the organizer, not title/attendees.
	organized := calendar.Event{
		ID: "o1", Summary: "통화", Start: end.Add(-time.Hour), End: end, Status: "confirmed",
		Organizer: calendar.Attendee{DisplayName: "영산고 행정실", Email: "admin@school.example"},
	}
	got := decideHarvests(now, []calendar.Event{organized}, nil, 0, harvestTestMatcher, harvestKST)
	if len(got) != 1 || got[0].Target != "영산고" {
		t.Errorf("organizer-carried target must match, got %+v", got)
	}

	// Email domains stay out of the match text: only the local part may feed
	// the (loose) matcher, so "co"/"kr" fragments cannot bind a personal
	// invite to an unrelated project.
	var seen []string
	spy := func(text string) string {
		seen = append(seen, text)
		return ""
	}
	personal := calendar.Event{
		ID: "p1", Summary: "저녁 약속", Start: end.Add(-time.Hour), End: end, Status: "confirmed",
		Attendees: []calendar.Attendee{{Email: "friend@partner.co.kr", DisplayName: ""}},
	}
	_ = decideHarvests(now, []calendar.Event{personal}, nil, 0, spy, harvestKST)
	if len(seen) != 1 {
		t.Fatalf("matcher calls = %d, want 1", len(seen))
	}
	if strings.Contains(seen[0], "co.kr") || strings.Contains(seen[0], "@") {
		t.Errorf("match text must carry the email local part only, got %q", seen[0])
	}
	if !strings.Contains(seen[0], "friend") {
		t.Errorf("match text must keep the email local part, got %q", seen[0])
	}
}

// TestIsMeetingShaped_RealCalendarSample pins the shape gate against the
// operator's ACTUAL calendar titles (17-event sample, 2026-07-05): the
// dominant pattern is "<회사> <이름> <직함>" with no meeting word, which the
// job-title list must catch. Bare "<회사> <이름>" without any evidence stays
// silent — documented gap, second-locked by the target matcher anyway.
func TestIsMeetingShaped_RealCalendarSample(t *testing.T) {
	shaped := []string{
		"프라임에너지 대표 미팅",
		"6/25(목) 11시 LG 방문 (계약서 검토 및 현물 확인)",
		"바로 및 기자재 회식",
		"비금도 해저케이블 포설 견학",
		"한화 유성민 팀장",
		"singsun 동사장",
		"현대차 esg 평가위원",
		"간납사 관련 pwc 미팅",
	}
	for _, s := range shaped {
		if !isMeetingShaped(calendar.Event{Summary: s}) {
			t.Errorf("real meeting title not shaped: %q", s)
		}
	}
	notShaped := []string{"영산고 발주", "견적서 제출", "잔금 송금", "징코 유정영"}
	for _, s := range notShaped {
		if isMeetingShaped(calendar.Event{Summary: s}) {
			t.Errorf("non-meeting title shaped: %q", s)
		}
	}
	// Structural evidence lifts a bare-name title into shape.
	if !isMeetingShaped(calendar.Event{Summary: "한솔 육종태", Location: "한솔 본사"}) {
		t.Error("location evidence must shape a bare-name event")
	}
}

// TestLooseUniqueNameMatch: terse titles resolve only via a UNIQUE token
// containment over projects+ledgers jointly — ambiguous tokens (당진 spans
// three projects; lg spans a project and a ledger) resolve to nothing rather
// than guessing.
func TestLooseUniqueNameMatch(t *testing.T) {
	names := harvestKnownNames(
		[]wiki.ProjectRef{
			{Name: "비금도-154kv-케이블-및-액세서리-(ztt)"},
			{Name: "당진-솔라빌리지"},
			{Name: "lg화학-당진"},
			{Name: "대한전선-당진"},
		},
		[]wiki.CounterpartyRef{
			{Name: "JA Solar"},
			{Name: "LG전자"},
		},
	)
	if got := looseUniqueNameMatch("비금도 해저케이블 포설 견학", names); got != "비금도-154kv-케이블-및-액세서리-(ztt)" {
		t.Errorf("unique token match = %q", got)
	}
	if got := looseUniqueNameMatch("당진 방문", names); got != "" {
		t.Errorf("ambiguous token must not resolve, got %q", got)
	}
	// Short vendor abbreviation resolves through the ledger list ("JA 이용원
	// 상무" — the real calendar pattern that exact matching misses).
	if got := looseUniqueNameMatch("JA 이용원 상무 재고모듈 구매 논의", names); got != "JA Solar" {
		t.Errorf("ledger token match = %q", got)
	}
	// "lg" spans a project (lg화학-당진) AND a ledger (LG전자) → joint
	// ambiguity, no guess.
	if got := looseUniqueNameMatch("LG 방문", names); got != "" {
		t.Errorf("cross-list ambiguous token must not resolve, got %q", got)
	}
	if got := looseUniqueNameMatch("점심 약속", names); got != "" {
		t.Errorf("no-token text must not resolve, got %q", got)
	}
	// Fidelity-drill regressions: a date fragment ("25") must not latch onto a
	// project named "…(2026-06-25)", and 2-rune Hangul function words (바로)
	// must not latch onto ledger names containing them.
	dated := append(names, "부산항터미널-태양광-(신선대)-—-가배치-요청-(2026-06-25)", "효성중공업, 바로(주)")
	if got := looseUniqueNameMatch("6/25(목) 11시 OO 방문", dated); got != "" {
		t.Errorf("digit token must not resolve, got %q", got)
	}
	if got := looseUniqueNameMatch("계약서 바로 검토 회의", dated); got != "" {
		t.Errorf("2-rune Hangul function word must not resolve, got %q", got)
	}
	// Generic nouns embedded in a descriptive slug TAIL must not match: only
	// the leading entity segments of a name participate.
	slugged := append(names, "강진-신다산-epc-계약서-법무검토-의견-(2026-06-30)")
	if got := looseUniqueNameMatch("LG 방문 계약서 검토", slugged); got != "" {
		t.Errorf("descriptive-tail noun must not resolve, got %q", got)
	}
	if got := looseUniqueNameMatch("강진 신다산 현장 회의", slugged); got != "강진-신다산-epc-계약서-법무검토-의견-(2026-06-30)" {
		t.Errorf("leading entity segment should still resolve, got %q", got)
	}
}

func TestHarvestStatePersistence(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 4}))
	statePath := filepath.Join(t.TempDir(), harvestStateFile)
	deliver := func(string) (bool, error) { return true, nil }
	resolve := func() (briefingCalendarClient, error) { return nil, nil }

	s := newMeetingHarvestService(deliver, resolve, harvestTestMatcher, statePath, logger)
	now := time.Now()
	s.markAsked("ev1|123", now)
	s.markAsked("old|456", now.Add(-48*time.Hour))

	// A fresh service (post-restart) must remember the asked keys.
	s2 := newMeetingHarvestService(deliver, resolve, harvestTestMatcher, statePath, logger)
	if !s2.alreadyAsked("ev1|123") || !s2.alreadyAsked("old|456") {
		t.Fatal("asked keys must survive restart")
	}
	// Daily cap counts TODAY's asks only.
	if got := s2.askedToday(now); got != 1 {
		t.Errorf("askedToday = %d, want 1", got)
	}
	// Retention prune drops entries older than the window.
	s2.pruneState(now.Add(harvestStateRetention + time.Minute))
	if s2.alreadyAsked("ev1|123") {
		t.Error("pruned key should be forgotten")
	}
}
