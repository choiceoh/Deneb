package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	asked := func(key string) bool { return key == harvestKey(evAsked) }
	events := []calendar.Event{evA, evFresh, evOld, evAllDay, evCancelled, evPersonal, evAsked, evOlder}

	got := decideHarvests(now, events, asked, 0, harvestTestMatcher, harvestKST)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want 2 (a, h)", got)
	}
	// Oldest end first: h (90min ago) before a (30min ago).
	if got[0].Event.ID != "h" || got[1].Event.ID != "a" {
		t.Errorf("order = %s,%s want h,a", got[0].Event.ID, got[1].Event.ID)
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
