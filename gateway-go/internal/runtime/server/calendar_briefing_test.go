package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// makeService builds a bare service struct sufficient for decision /
// dedup / format tests — no telegram plugin, no resolver, no goroutine.
// Tests call decidePushes / formatBriefing / alreadySent directly.
func makeService(t *testing.T) *calendarBriefingService {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Seoul")
	if loc == nil {
		loc = time.FixedZone("KST", kstFallbackOffset)
	}
	return &calendarBriefingService{
		leadTime:   15 * time.Minute,
		pollEvery:  1 * time.Minute,
		sent:       make(map[string]time.Time),
		displayLoc: loc,
	}
}

// runDecision wraps decidePushes + markSent so tests preserve the
// existing call-shape. The behavior under test lives in the pure
// decidePushes function which production tick() also calls — no test
// "mirror" can drift from production now.
func (s *calendarBriefingService) runDecision(now time.Time, events []calendar.Event) []calendar.Event {
	pushes := decidePushes(now, s.leadTime, events, s.alreadySent)
	for _, ev := range pushes {
		s.markSent(dedupKey(ev), ev.Start)
	}
	s.prune(now)
	return pushes
}

func TestBriefing_PushesEventInWindow(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "in-window", Start: now.Add(15 * time.Minute), Summary: "박YY"},
	}
	got := s.runDecision(now, events)
	if len(got) != 1 || got[0].ID != "in-window" {
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
	if len(got) != 1 || got[0].ID != "in-window" {
		t.Errorf("expected exactly one push (in-window), got %+v", got)
	}
}

func TestBriefing_HitsEdgesOfWindow(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "low-edge", Start: now.Add(13 * time.Minute)},
		{ID: "high-edge", Start: now.Add(17 * time.Minute)},
	}
	got := s.runDecision(now, events)
	if len(got) != 2 {
		t.Errorf("expected both edges to push, got %+v", got)
	}
}

func TestBriefing_DedupsRepeatedTicks(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "ev1", Start: now.Add(15 * time.Minute)},
	}
	first := s.runDecision(now, events)
	if len(first) != 1 {
		t.Fatalf("first tick expected 1 push: %+v", first)
	}
	second := s.runDecision(now, events)
	if len(second) != 0 {
		t.Fatalf("second tick should dedup, got %+v", second)
	}
}

// Rescheduled event: same Google ID, different Start. The new start
// must produce a new dedup key so the rescheduled briefing fires.
func TestBriefing_RescheduledEventGetsNewBriefing(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	original := calendar.Event{ID: "ev1", Start: now.Add(15 * time.Minute)}
	first := s.runDecision(now, []calendar.Event{original})
	if len(first) != 1 {
		t.Fatalf("first push missing: %+v", first)
	}

	// User reschedules to a different start time still in-window.
	// Same Google ID, different Start.Unix() → different dedup key.
	rescheduled := calendar.Event{ID: "ev1", Start: now.Add(16 * time.Minute)}
	second := s.runDecision(now, []calendar.Event{rescheduled})
	if len(second) != 1 {
		t.Fatalf("rescheduled event should re-push, got %+v", second)
	}
}

func TestBriefing_AllDayEventsIgnored(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "all-day", Start: now.Add(15 * time.Minute), AllDay: true},
	}
	got := s.runDecision(now, events)
	if len(got) != 0 {
		t.Errorf("all-day events should never trigger briefings, got %+v", got)
	}
}

func TestBriefing_ZeroStartTimeIgnored(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)
	events := []calendar.Event{{ID: "broken", Start: time.Time{}}}
	got := s.runDecision(now, events)
	if len(got) != 0 {
		t.Errorf("zero start time should be skipped, got %+v", got)
	}
}

func TestBriefing_PruneRemovesStaleEntries(t *testing.T) {
	s := makeService(t)
	now := time.Date(2026, 5, 26, 13, 45, 0, 0, time.UTC)

	recentEv := calendar.Event{ID: "recent", Start: now.Add(-5 * time.Minute)}
	staleEv := calendar.Event{ID: "stale", Start: now.Add(-2 * time.Hour)}
	s.markSent(dedupKey(recentEv), recentEv.Start)
	s.markSent(dedupKey(staleEv), staleEv.Start)
	s.prune(now)

	if !s.alreadySent(dedupKey(recentEv)) {
		t.Error("recent entry was incorrectly pruned")
	}
	if s.alreadySent(dedupKey(staleEv)) {
		t.Error("stale entry was not pruned")
	}
}

func TestFormatBriefing_KoreanShape(t *testing.T) {
	s := makeService(t)
	ev := calendar.Event{
		Summary:  "박YY 미팅",
		Location: "강남 본사",
		Start:    time.Date(2026, 5, 26, 14, 0, 0, 0, s.displayLoc),
		Attendees: []calendar.Attendee{
			{Email: "self@example.com", Self: true, DisplayName: "Me"},
			{Email: "p@example.com", DisplayName: "박YY", ResponseStatus: "accepted"},
			{Email: "k@example.com", DisplayName: "김ZZ", ResponseStatus: "needsAction"},
		},
	}
	body := s.formatBriefing(ev)

	for _, must := range []string{"D-15분", "박YY 미팅", "14:00", "강남 본사", "박YY", "김ZZ"} {
		if !strings.Contains(body, must) {
			t.Errorf("briefing missing %q in:\n%s", must, body)
		}
	}
	if strings.Contains(body, "Me") {
		t.Errorf("self attendee leaked into briefing:\n%s", body)
	}
}

func TestFormatBriefing_FiltersDeclinedAttendees(t *testing.T) {
	s := makeService(t)
	ev := calendar.Event{
		Summary: "회의",
		Start:   time.Date(2026, 5, 26, 14, 0, 0, 0, s.displayLoc),
		Attendees: []calendar.Attendee{
			{DisplayName: "박YY", ResponseStatus: "accepted"},
			{DisplayName: "김ZZ", ResponseStatus: "declined"},
			{DisplayName: "이QQ", ResponseStatus: "tentative"},
		},
	}
	body := s.formatBriefing(ev)
	if !strings.Contains(body, "박YY") || !strings.Contains(body, "이QQ") {
		t.Errorf("expected accepted/tentative attendees, got:\n%s", body)
	}
	if strings.Contains(body, "김ZZ") {
		t.Errorf("declined attendee should be filtered, got:\n%s", body)
	}
}

func TestFormatBriefing_HandlesMissingTitle(t *testing.T) {
	s := makeService(t)
	body := s.formatBriefing(calendar.Event{Start: time.Now()})
	if !strings.Contains(body, "(제목 없음)") {
		t.Errorf("missing-title fallback not present:\n%s", body)
	}
}

// HTML escape: telegram.Plugin sends the briefing with ParseMode "HTML"
// (plugin.go), so any raw "<"/">"/"&" in calendar fields would make
// Telegram reject the message with an entity-parse error and the D-15
// push would silently fail every tick. The escape must happen inside
// formatBriefing — anything that survives to the wire breaks delivery.
func TestFormatBriefing_EscapesHTMLEntities(t *testing.T) {
	s := makeService(t)
	ev := calendar.Event{
		Summary:  "Q1 <리뷰> & 계획",
		Location: "회의실 A <3F>",
		Start:    time.Date(2026, 5, 26, 14, 0, 0, 0, s.displayLoc),
		Attendees: []calendar.Attendee{
			{DisplayName: "이름 <bracket>", ResponseStatus: "accepted"},
		},
	}
	body := s.formatBriefing(ev)
	for _, leak := range []string{"<리뷰>", "<3F>", "<bracket>"} {
		if strings.Contains(body, leak) {
			t.Errorf("raw HTML entity %q must be escaped, got:\n%s", leak, body)
		}
	}
	for _, must := range []string{"&lt;리뷰&gt;", "&lt;3F&gt;", "&lt;bracket&gt;"} {
		if !strings.Contains(body, must) {
			t.Errorf("expected escaped %q in:\n%s", must, body)
		}
	}
}

// FixedZone fallback: when LoadLocation fails, the service uses a
// +09:00 offset, so a 14:00 KST event renders as "14:00" — never UTC.
func TestFormatBriefing_FixedZoneFallbackRendersKSTWallClock(t *testing.T) {
	s := makeService(t)
	s.displayLoc = time.FixedZone("KST", kstFallbackOffset)
	// 14:00 KST == 05:00 UTC. Encode the start in UTC to verify the
	// briefing displays the KST wall clock (not UTC).
	ev := calendar.Event{
		Summary: "x",
		Start:   time.Date(2026, 5, 26, 5, 0, 0, 0, time.UTC),
	}
	body := s.formatBriefing(ev)
	if !strings.Contains(body, "14:00") {
		t.Errorf("expected 14:00 wall-clock (KST fallback), got:\n%s", body)
	}
}

func TestNewCalendarBriefingService_NilDepsAreNoOp(t *testing.T) {
	got := newCalendarBriefingService(nil, func() (briefingCalendarClient, error) { return nil, nil }, nil)
	if got != nil {
		t.Error("expected nil service when telegram plugin missing")
	}
	got = newCalendarBriefingService(nil, nil, nil)
	if got != nil {
		t.Error("expected nil service when resolver missing")
	}
}

func TestNilServiceStartIsSafe(t *testing.T) {
	var s *calendarBriefingService
	s.start(context.Background())
}

// Race guard: dedup map must serialize concurrent markSent /
// alreadySent — exercised under -race.
func TestBriefing_DedupMapRaceFree(t *testing.T) {
	s := makeService(t)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := dedupKey(calendar.Event{ID: "ev", Start: time.Unix(int64(i), 0)})
			if s.alreadySent(key) {
				return
			}
			s.markSent(key, time.Now())
		}(i)
	}
	wg.Wait()
}

// Resolve-failure throttle: the first failure logs Warn; identical
// follow-ups within the same outage are suppressed. Successful resolve
// clears the throttle.
func TestBriefing_ResolveFailureThrottle(t *testing.T) {
	s := makeService(t)
	// First call → first==true.
	s.resolveFailureMu.Lock()
	first := !s.resolveFailing
	s.resolveFailing = true
	s.resolveFailureMu.Unlock()
	if !first {
		t.Fatal("first failure should be 'first'")
	}
	// Second call → first==false (suppressed).
	s.resolveFailureMu.Lock()
	second := !s.resolveFailing
	s.resolveFailureMu.Unlock()
	if second {
		t.Fatal("second failure should be suppressed")
	}
	// Recovery → throttle resets.
	s.clearResolveFailure()
	s.resolveFailureMu.Lock()
	third := !s.resolveFailing
	s.resolveFailureMu.Unlock()
	if !third {
		t.Fatal("post-recovery failure should be 'first' again")
	}
}

func TestIsTelegramNotReady(t *testing.T) {
	if !isTelegramNotReady(errors.New("telegram client not initialized")) {
		t.Error("expected match for production error string")
	}
	if !isTelegramNotReady(errTelegramNotReady) {
		t.Error("expected match for sentinel via errors.Is")
	}
	if isTelegramNotReady(errors.New("send text: HTTP 500")) {
		t.Error("unrelated error should not match")
	}
}
