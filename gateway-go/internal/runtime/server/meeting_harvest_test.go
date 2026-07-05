package server

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

// TestLooseUniqueProjectMatch: terse titles resolve to a project only via a
// UNIQUE token containment — ambiguous tokens (당진 spans three projects)
// resolve to nothing rather than guessing.
func TestLooseUniqueProjectMatch(t *testing.T) {
	projects := []wiki.ProjectRef{
		{Name: "비금도-154kv-케이블-및-액세서리-(ztt)"},
		{Name: "당진-솔라빌리지"},
		{Name: "lg화학-당진"},
		{Name: "대한전선-당진"},
	}
	if got := looseUniqueProjectMatch("비금도 해저케이블 포설 견학", projects); got != "비금도-154kv-케이블-및-액세서리-(ztt)" {
		t.Errorf("unique token match = %q", got)
	}
	if got := looseUniqueProjectMatch("당진 방문", projects); got != "" {
		t.Errorf("ambiguous token must not resolve, got %q", got)
	}
	if got := looseUniqueProjectMatch("lg화학 당진 미팅", projects); got != "lg화학-당진" {
		t.Errorf("compound token should disambiguate, got %q", got)
	}
	if got := looseUniqueProjectMatch("점심 약속", projects); got != "" {
		t.Errorf("no-token text must not resolve, got %q", got)
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
