package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// The briefing service is structured so its critical decision logic
// (window-match, dedup, prune) is testable in isolation via runDecision
// below — we avoid faking the full Telegram plugin surface area.
//
// The test exercises:
//   - runDecision: the per-tick body, given (now, events), returns which
//     events would be pushed and updates the dedup map exactly as in
//     production.
//   - alreadySent / markSent / prune: the dedup map lifecycle.

type briefingDecision struct {
	pushed []calendar.Event
}

// runDecision mimics the per-tick logic without doing any I/O.
// Mirrors the production tick() body exactly so a change there is the
// only way for these guarantees to drift.
func (s *calendarBriefingService) runDecision(now time.Time, events []calendar.Event) briefingDecision {
	windowMin := now.Add(s.leadTime - calendarWindowSlack)
	windowMax := now.Add(s.leadTime + calendarWindowSlack)
	var out briefingDecision
	for _, ev := range events {
		if ev.AllDay {
			continue
		}
		if ev.Start.IsZero() || ev.Start.Before(windowMin) || ev.Start.After(windowMax) {
			continue
		}
		if s.alreadySent(ev.ID) {
			continue
		}
		out.pushed = append(out.pushed, ev)
		s.markSent(ev.ID, ev.Start)
	}
	s.prune(now)
	return out
}

func makeService(t *testing.T) *calendarBriefingService {
	t.Helper()
	return &calendarBriefingService{
		leadTime:  15 * time.Minute,
		pollEvery: 1 * time.Minute,
		sent:      make(map[string]time.Time),
	}
}

func TestBriefing_PushesEventInWindow(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "in-window", Start: now.Add(15 * time.Minute), Summary: "박YY"},
	}
	got := s.runDecision(now, events)
	if len(got.pushed) != 1 || got.pushed[0].ID != "in-window" {
		t.Errorf("expected one push, got %+v", got)
	}
}

func TestBriefing_SkipsOutsideWindow(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "too-soon", Start: now.Add(5 * time.Minute)},   // < window
		{ID: "too-far", Start: now.Add(30 * time.Minute)},   // > window
		{ID: "in-window", Start: now.Add(15 * time.Minute)}, // hit
	}
	got := s.runDecision(now, events)
	if len(got.pushed) != 1 || got.pushed[0].ID != "in-window" {
		t.Errorf("expected exactly one push (in-window), got %+v", got)
	}
}

func TestBriefing_HitsEdgesOfWindow(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	// 15-2 = 13min and 15+2 = 17min are the exact edges; both should hit.
	events := []calendar.Event{
		{ID: "low-edge", Start: now.Add(13 * time.Minute)},
		{ID: "high-edge", Start: now.Add(17 * time.Minute)},
	}
	got := s.runDecision(now, events)
	if len(got.pushed) != 2 {
		t.Errorf("expected both edges to push, got %+v", got)
	}
}

func TestBriefing_DedupsRepeatedTicks(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "ev1", Start: now.Add(15 * time.Minute)},
	}
	// First tick → push. Second tick at same now → skip.
	first := s.runDecision(now, events)
	if len(first.pushed) != 1 {
		t.Fatalf("first tick expected 1 push: %+v", first)
	}
	second := s.runDecision(now, events)
	if len(second.pushed) != 0 {
		t.Fatalf("second tick should dedup, got %+v", second)
	}
}

func TestBriefing_AllDayEventsIgnored(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "all-day", Start: now.Add(15 * time.Minute), AllDay: true},
	}
	got := s.runDecision(now, events)
	if len(got.pushed) != 0 {
		t.Errorf("all-day events should never trigger briefings, got %+v", got)
	}
}

func TestBriefing_ZeroStartTimeIgnored(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{{ID: "broken", Start: time.Time{}}}
	got := s.runDecision(now, events)
	if len(got.pushed) != 0 {
		t.Errorf("zero start time should be skipped, got %+v", got)
	}
}

func TestBriefing_PruneRemovesStaleEntries(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)

	s.markSent("recent", now.Add(-5*time.Minute)) // -5min: still fresh
	s.markSent("stale", now.Add(-2*time.Hour))    // -2h: stale, evict
	s.prune(now)

	if !s.alreadySent("recent") {
		t.Error("recent entry was incorrectly pruned")
	}
	if s.alreadySent("stale") {
		t.Error("stale entry was not pruned")
	}
}

func TestFormatBriefing_KoreanShape(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	ev := calendar.Event{
		Summary:    "박YY 미팅",
		Location:   "강남 본사",
		Start:      time.Date(2026, 5, 26, 14, 0, 0, 0, loc),
		Conference: &calendar.ConferenceInfo{URI: "https://meet.google.com/abc"},
		Attendees: []calendar.Attendee{
			{Email: "self@example.com", Self: true, DisplayName: "Me"},
			{Email: "p@example.com", DisplayName: "박YY"},
			{Email: "k@example.com", DisplayName: "김ZZ"},
		},
	}
	body := formatBriefing(ev, 15*time.Minute)

	for _, must := range []string{"D-15분", "박YY 미팅", "14:00", "강남 본사", "meet.google.com/abc", "박YY", "김ZZ"} {
		if !strings.Contains(body, must) {
			t.Errorf("briefing missing %q in:\n%s", must, body)
		}
	}
	if strings.Contains(body, "Me") {
		t.Errorf("self attendee leaked into briefing:\n%s", body)
	}
}

func TestFormatBriefing_HandlesMissingTitle(t *testing.T) {
	body := formatBriefing(calendar.Event{Start: time.Now()}, 15*time.Minute)
	if !strings.Contains(body, "(제목 없음)") {
		t.Errorf("missing-title fallback not present:\n%s", body)
	}
}

func TestNewCalendarBriefingService_NilDepsAreNoOp(t *testing.T) {
	// No telegram plugin → nil.
	got := newCalendarBriefingService(nil, func() (briefingCalendarClient, error) { return nil, nil }, nil)
	if got != nil {
		t.Error("expected nil service when telegram plugin missing")
	}
	// No resolver → nil.
	got = newCalendarBriefingService(nil, nil, nil)
	if got != nil {
		t.Error("expected nil service when resolver missing")
	}
}

func TestNilServiceStartIsSafe(t *testing.T) {
	// Production code calls start() unconditionally; this verifies the
	// receiver nil-guard works so we don't panic on a no-config gateway.
	var s *calendarBriefingService
	s.start(context.Background())
}

// Race guard: dedup map must serialize concurrent markSent / alreadySent
// — exercised under -race. Goroutines simulate two ticks landing very
// close together (cron jitter + manual /reload).
func TestBriefing_DedupMapRaceFree(t *testing.T) {
	s := makeService(t)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "ev"
			if s.alreadySent(id) {
				return
			}
			s.markSent(id, time.Now())
		}(i)
	}
	wg.Wait()
}
