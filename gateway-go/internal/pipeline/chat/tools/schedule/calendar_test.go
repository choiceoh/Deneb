package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// fakeCalReader stands in for the read-only Google client.
type fakeCalReader struct {
	upcoming []tooldeps.CalendarEvent
	byID     map[string]*tooldeps.CalendarEvent
	listErr  error
}

func (f *fakeCalReader) ListUpcoming(_ context.Context, _, _ time.Time, _ int) ([]tooldeps.CalendarEvent, error) {
	return f.upcoming, f.listErr
}

func (f *fakeCalReader) Get(_ context.Context, id string) (*tooldeps.CalendarEvent, error) {
	return f.byID[id], nil
}

type blockingCalReader struct {
	started chan struct{}
	release chan struct{}
	events  []tooldeps.CalendarEvent
}

func (f *blockingCalReader) ListUpcoming(ctx context.Context, _, _ time.Time, _ int) ([]tooldeps.CalendarEvent, error) {
	close(f.started)
	select {
	case <-f.release:
		return f.events, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *blockingCalReader) Get(_ context.Context, _ string) (*tooldeps.CalendarEvent, error) {
	return nil, nil
}

type observingLocalCal struct {
	started chan struct{}
	events  []tooldeps.CalendarEvent
}

func (l *observingLocalCal) ListRange(_, _ time.Time) []tooldeps.CalendarEvent {
	close(l.started)
	return l.events
}

func (l *observingLocalCal) Get(string) *tooldeps.CalendarEvent { return nil }

func (l *observingLocalCal) Create(tooldeps.CalendarCreateInput) (tooldeps.CalendarEvent, error) {
	return tooldeps.CalendarEvent{}, errors.New("not implemented")
}

func (l *observingLocalCal) Update(string, tooldeps.CalendarCreateInput) (*tooldeps.CalendarEvent, error) {
	return nil, errors.New("not implemented")
}

func (l *observingLocalCal) Delete(string) error { return errors.New("not implemented") }

func newTestLocalCal(t *testing.T) *localcal.Store {
	t.Helper()
	store, err := localcal.New(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatalf("localcal.New: %v", err)
	}
	return store
}

func callCal(t *testing.T, d *tooldeps.CalendarDeps, params map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := ToolCalendar(d)(context.Background(), raw)
	if err != nil {
		t.Fatalf("tool err: %v", err)
	}
	return out
}

// extractCalID pulls the "id=..." token out of a tool response (no spaces in IDs).
func extractCalID(s string) string {
	i := strings.Index(s, "id=")
	if i < 0 {
		return ""
	}
	rest := s[i+3:]
	if j := strings.IndexAny(rest, " \n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// readerWith returns deps with a Google reader + local store.
func depsWith(reader tooldeps.CalendarReader, local tooldeps.LocalCalendar) *tooldeps.CalendarDeps {
	return &tooldeps.CalendarDeps{
		Client: func() (tooldeps.CalendarReader, error) { return reader, nil },
		Local:  local,
	}
}

func TestCalendarListReturnsMergedEventsSortedByStart(t *testing.T) {
	now := time.Now()
	google := &fakeCalReader{
		upcoming: []tooldeps.CalendarEvent{
			{ID: "g-event-1", Summary: "구글 주간회의", Start: now.Add(2 * time.Hour), End: now.Add(3 * time.Hour)},
		},
	}
	local := newTestLocalCal(t)
	if _, err := local.Create(localcal.CreateInput{Summary: "로컬 통화", Start: now.Add(1 * time.Hour)}); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	out := callCal(t, depsWith(google, wrapTestLocalCal(local)), map[string]any{"action": "list"})

	if !strings.Contains(out, "로컬 통화") || !strings.Contains(out, "구글 주간회의") {
		t.Fatalf("expected both events in list, got:\n%s", out)
	}
	// local (T+1h) must sort before google (T+2h).
	if strings.Index(out, "로컬 통화") > strings.Index(out, "구글 주간회의") {
		t.Errorf("events not sorted by start time:\n%s", out)
	}
}

func TestCalMergedReadsGoogleAndLocalConcurrently(t *testing.T) {
	now := time.Now()
	google := &blockingCalReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events: []tooldeps.CalendarEvent{
			{ID: "g-event-1", Summary: "구글 주간회의", Start: now.Add(2 * time.Hour)},
		},
	}
	local := &observingLocalCal{
		started: make(chan struct{}),
		events: []tooldeps.CalendarEvent{
			{ID: "local:event-1", Summary: "로컬 통화", Start: now.Add(time.Hour)},
		},
	}
	released := false
	releaseGoogle := func() {
		if !released {
			close(google.release)
			released = true
		}
	}
	defer releaseGoogle()

	var events []tooldeps.CalendarEvent
	var warn string
	done := make(chan struct{})
	go func() {
		events, warn = calMerged(context.Background(), depsWith(google, local), now, now.Add(4*time.Hour))
		close(done)
	}()

	waitForSignal(t, "google list start", google.started)
	waitForSignal(t, "local list start while google is still blocked", local.started)

	releaseGoogle()
	waitForSignal(t, "merged calendar result", done)
	if warn != "" {
		t.Fatalf("warn = %q", warn)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want google + local", events)
	}
	if events[0].Summary != "로컬 통화" || events[1].Summary != "구글 주간회의" {
		t.Fatalf("events not merged and sorted: %+v", events)
	}
}

func waitForSignal(t *testing.T, name string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func TestCalendar_CreateGetUpdateDelete(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)

	// Create.
	createOut := callCal(t, d, map[string]any{
		"action":   "create",
		"summary":  "데네브 미팅",
		"start":    start.Format(time.RFC3339),
		"location": "회의실 A",
	})
	if !strings.Contains(createOut, "추가했습니다") || !strings.Contains(createOut, "데네브 미팅") {
		t.Fatalf("create failed:\n%s", createOut)
	}
	id := extractCalID(createOut)
	if !localcal.IsLocalID(id) {
		t.Fatalf("expected local id, got %q from:\n%s", id, createOut)
	}

	// Get returns rich detail (location).
	getOut := callCal(t, d, map[string]any{"action": "get", "id": id})
	if !strings.Contains(getOut, "회의실 A") || !strings.Contains(getOut, "데네브 미팅") {
		t.Errorf("get detail missing fields:\n%s", getOut)
	}

	// Update the summary.
	updOut := callCal(t, d, map[string]any{
		"action":  "update",
		"id":      id,
		"summary": "데네브 미팅 (수정)",
		"start":   start.Format(time.RFC3339),
	})
	if !strings.Contains(updOut, "수정했습니다") || !strings.Contains(updOut, "데네브 미팅 (수정)") {
		t.Errorf("update failed:\n%s", updOut)
	}

	// Delete.
	delOut := callCal(t, d, map[string]any{"action": "delete", "id": id})
	if !strings.Contains(delOut, "삭제했습니다") {
		t.Errorf("delete failed:\n%s", delOut)
	}
	// Gone now.
	if ev := local.Get(id); ev != nil {
		t.Errorf("event still present after delete: %+v", ev)
	}
}

func TestCalendarCaptureReturnsMinutesGuidanceForEvent(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)

	createOut := callCal(t, d, map[string]any{
		"action":  "create",
		"summary": "현대차 견적 회의",
		"start":   start.Format(time.RFC3339),
	})
	id := extractCalID(createOut)

	out := callCal(t, d, map[string]any{"action": "capture", "id": id})
	// Capture returns minutes guidance that names the event, instructs the
	// writeback to THIS event via update, and carries the delegation framing.
	for _, want := range []string{"회의록", "현대차 견적 회의", id, "update", "임원"} {
		if !strings.Contains(out, want) {
			t.Errorf("capture output missing %q:\n%s", want, out)
		}
	}
}

func TestCalendarAuditReturnsOverlapAndOverloadFindings(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	// Tomorrow, inside the default 7-day window + 09–18 working hours.
	base := time.Now().Add(24 * time.Hour)
	day := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location())
	mk := func(h, m int, title string) {
		start := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
		callCal(t, d, map[string]any{"action": "create", "summary": title, "start": start.Format(time.RFC3339)})
	}
	mk(10, 0, "A")
	mk(10, 30, "B") // overlaps A (default 1h) → double-booking
	mk(13, 0, "C")
	mk(14, 0, "D")
	mk(15, 0, "E")
	mk(16, 0, "F") // 6 meetings → overloaded day

	out := callCal(t, d, map[string]any{"action": "audit"})
	for _, want := range []string{"일정 점검", "겹침", "과부하"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit output missing %q:\n%s", want, out)
		}
	}
}

func TestCalendarAuditReturnsCleanForEmptyCalendar(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	out := callCal(t, d, map[string]any{"action": "audit"})
	if !strings.Contains(out, "일정 양호") {
		t.Errorf("empty calendar should audit clean, got:\n%s", out)
	}
}

func TestCalendarTimelineReturnsOnlyQueryMatchedEvents(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	day := time.Now().Add(48 * time.Hour)
	mk := func(title string) {
		start := time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, day.Location())
		callCal(t, d, map[string]any{"action": "create", "summary": title, "start": start.Format(time.RFC3339)})
	}
	mk("현대차 견적 회의")
	mk("내부 주간 회의") // unrelated to the query

	out := callCal(t, d, map[string]any{"action": "timeline", "query": "현대차"})
	if !strings.Contains(out, "타임라인") || !strings.Contains(out, "현대차 견적 회의") {
		t.Errorf("timeline missing header/matched event:\n%s", out)
	}
	if strings.Contains(out, "내부 주간 회의") {
		t.Errorf("timeline included an unrelated event:\n%s", out)
	}
}

func TestCalendarTimelineReturnsHintWhenQueryMissing(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	out := callCal(t, d, map[string]any{"action": "timeline"})
	if !strings.Contains(out, "query") {
		t.Errorf("expected a query-required hint, got:\n%s", out)
	}
}

func TestCalendar_TimelineRejectsBadRange(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	// Inverted from/to must be rejected, not silently widened to the default.
	out := callCal(t, d, map[string]any{
		"action": "timeline", "query": "현대차",
		"from": "2026-07-01T00:00:00+09:00",
		"to":   "2026-06-01T00:00:00+09:00",
	})
	if !strings.Contains(out, "뒤여야") {
		t.Errorf("expected inverted-range rejection, got:\n%s", out)
	}
}

func TestCalendarAuditNotCleanWhenGoogleFetchFails(t *testing.T) {
	// Google fetch fails → calMerged returns a warn with only local data, so the
	// audit must not certify the schedule clean.
	google := &fakeCalReader{listErr: errors.New("google down")}
	d := depsWith(google, wrapTestLocalCal(newTestLocalCal(t)))
	out := callCal(t, d, map[string]any{"action": "audit"})
	if strings.Contains(out, "일정 양호") {
		t.Errorf("partial-data audit must not certify clean:\n%s", out)
	}
	if !strings.Contains(out, "확인하지 못했") {
		t.Errorf("expected a partial-data caveat, got:\n%s", out)
	}
}

func TestCalendar_RejectGoogleWrite(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}

	updOut := callCal(t, d, map[string]any{
		"action": "update", "id": "g-readonly-1",
		"summary": "x", "start": time.Now().Format(time.RFC3339),
	})
	if !strings.Contains(updOut, "수정할 수 없") {
		t.Errorf("expected Google update rejection, got:\n%s", updOut)
	}

	delOut := callCal(t, d, map[string]any{"action": "delete", "id": "g-readonly-1"})
	if !strings.Contains(delOut, "삭제할 수 없") {
		t.Errorf("expected Google delete rejection, got:\n%s", delOut)
	}
}

func TestCalendarGetReturnsGoogleEventDetailAsReadOnly(t *testing.T) {
	google := &fakeCalReader{
		byID: map[string]*tooldeps.CalendarEvent{
			"g-1": {
				ID: "g-1", Summary: "외부 미팅",
				Start:      time.Now().Add(time.Hour),
				Attendees:  []tooldeps.CalendarAttendee{{DisplayName: "김민준", ResponseStatus: "accepted"}},
				Conference: &tooldeps.CalendarConference{Solution: "hangoutsMeet", URI: "https://meet.example/abc"},
			},
		},
	}
	d := depsWith(google, wrapTestLocalCal(newTestLocalCal(t)))
	out := callCal(t, d, map[string]any{"action": "get", "id": "g-1"})
	if !strings.Contains(out, "외부 미팅") || !strings.Contains(out, "김민준") || !strings.Contains(out, "수락") {
		t.Errorf("google get detail missing attendee/rsvp:\n%s", out)
	}
	if !strings.Contains(out, "meet.example") {
		t.Errorf("google get detail missing Meet link:\n%s", out)
	}
	if !strings.Contains(out, "읽기 전용") {
		t.Errorf("google event should be marked read-only:\n%s", out)
	}
}

func TestCalendar_UnknownAction(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	out := callCal(t, d, map[string]any{"action": "frobnicate"})
	if !strings.Contains(out, "알 수 없는 액션") {
		t.Errorf("expected unknown-action message, got:\n%s", out)
	}
}

func TestCalendar_ListEmpty(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	out := callCal(t, d, map[string]any{"action": "list"})
	if !strings.Contains(out, "일정이 없습니다") {
		t.Errorf("expected empty message, got:\n%s", out)
	}
}

func TestCalendarGlanceRendersEventsWithRelativeDayLabels(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	if loc == nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	// Anchor "now" at a fixed wall clock so relative labels are deterministic.
	now := time.Date(2026, 6, 9, 9, 0, 0, 0, loc)

	google := &fakeCalReader{
		upcoming: []tooldeps.CalendarEvent{
			{
				ID: "g-1", Summary: "주간회의", Start: now.Add(2 * time.Hour),
				Conference: &tooldeps.CalendarConference{URI: "https://meet.example/x"},
			},
		},
	}
	local := newTestLocalCal(t)
	if _, err := local.Create(localcal.CreateInput{Summary: "고객 통화", Location: "본사", Start: now.Add(26 * time.Hour)}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out := CalendarGlance(context.Background(), depsWith(google, wrapTestLocalCal(local)), now, 3)
	if !strings.Contains(out, "주간회의") || !strings.Contains(out, "고객 통화") {
		t.Fatalf("glance missing events:\n%s", out)
	}
	if !strings.Contains(out, "오늘") {
		t.Errorf("expected 오늘 label for same-day event:\n%s", out)
	}
	if !strings.Contains(out, "내일") {
		t.Errorf("expected 내일 label for next-day event:\n%s", out)
	}
	if !strings.Contains(out, "🎥Meet") || !strings.Contains(out, "📍본사") {
		t.Errorf("glance missing badges:\n%s", out)
	}
}

func TestCalendarGlance_EmptyWhenNoSource(t *testing.T) {
	if out := CalendarGlance(context.Background(), &tooldeps.CalendarDeps{}, time.Now(), 3); out != "" {
		t.Errorf("expected empty glance with no source, got:\n%s", out)
	}
}

func TestCalendar_FreeWithin(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	at := func(h, m int) time.Time { return time.Date(2026, 6, 10, h, m, 0, 0, loc) }

	busy := []interval{
		{at(10, 0), at(11, 0)},
		{at(14, 0), at(15, 30)},
		{at(14, 30), at(15, 0)}, // overlaps previous → should merge
	}
	gaps := freeWithin(at(9, 0), at(18, 0), busy, 30*time.Minute)
	if len(gaps) != 3 {
		t.Fatalf("expected 3 gaps, got %d: %+v", len(gaps), gaps)
	}
	want := []interval{{at(9, 0), at(10, 0)}, {at(11, 0), at(14, 0)}, {at(15, 30), at(18, 0)}}
	for i, w := range want {
		if !gaps[i].start.Equal(w.start) || !gaps[i].end.Equal(w.end) {
			t.Errorf("gap %d = %v–%v, want %v–%v", i, gaps[i].start, gaps[i].end, w.start, w.end)
		}
	}
}

func TestCalendar_FreeWithin_SkipsTooShort(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	at := func(h, m int) time.Time { return time.Date(2026, 6, 10, h, m, 0, 0, loc) }
	// Only a 20-min gap between the two events; minDur 30 → excluded.
	busy := []interval{{at(9, 0), at(10, 0)}, {at(10, 20), at(18, 0)}}
	gaps := freeWithin(at(9, 0), at(18, 0), busy, 30*time.Minute)
	if len(gaps) != 0 {
		t.Errorf("expected no qualifying gaps, got %+v", gaps)
	}
}

func TestDetectConflictsReturnsOverlappingPairs(t *testing.T) {
	now := time.Now()
	// Sorted by start, as calMerged returns.
	events := []tooldeps.CalendarEvent{
		{Summary: "A", Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour)},
		{Summary: "B", Start: now.Add(90 * time.Minute), End: now.Add(150 * time.Minute)}, // overlaps A
		{Summary: "C", Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour)},        // no overlap
	}
	conflicts := detectConflicts(events)
	if len(conflicts) != 1 || conflicts[0][0] != "A" || conflicts[0][1] != "B" {
		t.Errorf("expected one A↔B conflict, got %+v", conflicts)
	}
}

func TestCalendarFreeSlotsReturnsGapsAroundBusyAndLunch(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	// Tomorrow, so the "don't suggest past slots today" clamp never interferes.
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1)
	dayStart := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)

	local := newTestLocalCal(t)
	busyStart := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, loc)
	if _, err := local.Create(localcal.CreateInput{Summary: "점유", Start: busyStart, End: busyStart.Add(time.Hour)}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{
		"action": "free_slots",
		"from":   dayStart.Format(time.RFC3339),
		"to":     dayEnd.Format(time.RFC3339),
		// Tomorrow may be a weekend; the default weekend skip is tested separately.
		"include_weekends": true,
	})
	// Default working hours 09–18, busy 10–11, lunch 12–13 carved out
	// → expect 09:00–10:00, 11:00–12:00, 13:00–18:00.
	if !strings.Contains(out, "09:00–10:00") || !strings.Contains(out, "11:00–12:00") || !strings.Contains(out, "13:00–18:00") {
		t.Errorf("free slots not split around busy block + lunch:\n%s", out)
	}
}

// Weekends are skipped by default; include_weekends opts back in. A window
// covering exactly one Saturday yields no slots unless opted in.
func TestCalendarFreeSlotsIgnoresWeekendsUnlessOptedIn(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	day := time.Now().In(loc).AddDate(0, 0, 1)
	for day.Weekday() != time.Saturday {
		day = day.AddDate(0, 0, 1)
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	deps := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}

	out := callCal(t, deps, map[string]any{
		"action": "free_slots",
		"from":   dayStart.Format(time.RFC3339),
		"to":     dayEnd.Format(time.RFC3339),
	})
	if !strings.Contains(out, "빈 시간이 없습니다") {
		t.Errorf("expected no slots on a weekend by default:\n%s", out)
	}

	out = callCal(t, deps, map[string]any{
		"action":           "free_slots",
		"from":             dayStart.Format(time.RFC3339),
		"to":               dayEnd.Format(time.RFC3339),
		"include_weekends": true,
	})
	if !strings.Contains(out, "09:00–12:00") {
		t.Errorf("expected weekend slots with include_weekends=true:\n%s", out)
	}
}

// A deliberately narrow lunch-hour window is NOT carved out — an explicit
// "find me a lunch slot" search still works.
func TestCalendarFreeSlotsPreservesExplicitNarrowLunchWindow(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	day := time.Now().In(loc).AddDate(0, 0, 1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}, map[string]any{
		"action":    "free_slots",
		"from":      dayStart.Format(time.RFC3339),
		"to":        dayStart.Add(24 * time.Hour).Format(time.RFC3339),
		"day_start": 12,
		"day_end":   13,
	})
	if !strings.Contains(out, "12:00–13:00") {
		t.Errorf("narrow lunch window must not be carved out:\n%s", out)
	}
}

func TestCalendarListReturnsConflictFlagForOverlappingEvents(t *testing.T) {
	local := newTestLocalCal(t)
	base := time.Now().Add(2 * time.Hour)
	if _, err := local.Create(localcal.CreateInput{Summary: "회의 A", Start: base, End: base.Add(time.Hour)}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := local.Create(localcal.CreateInput{Summary: "회의 B", Start: base.Add(30 * time.Minute), End: base.Add(90 * time.Minute)}); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{"action": "list"})
	if !strings.Contains(out, "겹치는 일정") || !strings.Contains(out, "회의 A") || !strings.Contains(out, "회의 B") {
		t.Errorf("expected conflict flag for overlapping events:\n%s", out)
	}
}

func TestCalendar_CreateRequiresFields(t *testing.T) {
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(newTestLocalCal(t))}
	if out := callCal(t, d, map[string]any{"action": "create", "start": time.Now().Format(time.RFC3339)}); !strings.Contains(out, "summary") {
		t.Errorf("expected summary-required, got:\n%s", out)
	}
	if out := callCal(t, d, map[string]any{"action": "create", "summary": "x"}); !strings.Contains(out, "start") {
		t.Errorf("expected start-required, got:\n%s", out)
	}
}

func TestCalLinkBadgeFormatsKindAndLabel(t *testing.T) {
	cases := []struct {
		name string
		ev   tooldeps.CalendarEvent
		want string
	}{
		{"none", tooldeps.CalendarEvent{Summary: "x"}, ""},
		{"kind only", tooldeps.CalendarEvent{Kind: "meeting"}, " · [미팅]"},
		{"label only", tooldeps.CalendarEvent{SourceLabel: "비금 통관"}, " · 「비금 통관」"},
		{"kind+label", tooldeps.CalendarEvent{Kind: "deadline", SourceLabel: "납기"}, " · [기한] 「납기」"},
	}
	for _, c := range cases {
		if got := calLinkBadge(c.ev); got != c.want {
			t.Errorf("%s: calLinkBadge = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCalendarBriefRendersLinkBadgeAndSynthesisDirective(t *testing.T) {
	local := newTestLocalCal(t)
	start := time.Now().In(calDisplayLoc()).Add(2 * time.Hour)
	if _, err := local.Create(localcal.CreateInput{
		Summary:     "ZTT 미팅",
		Start:       start,
		End:         start.Add(time.Hour),
		Source:      "mail:abc123",
		SourceLabel: "비금 154kV 통관",
		Kind:        "meeting",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Explicit window so the test is deterministic regardless of wall-clock.
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{
		"action": "brief",
		"from":   start.Add(-time.Hour).Format(time.RFC3339),
		"to":     start.Add(2 * time.Hour).Format(time.RFC3339),
	})
	if !strings.Contains(out, "[미팅]") || !strings.Contains(out, "비금 154kV 통관") {
		t.Errorf("brief missing link badge:\n%s", out)
	}
	if !strings.Contains(out, "브리핑으로 정리") {
		t.Errorf("brief missing synthesis directive:\n%s", out)
	}
}

func TestCalendarPrepReturnsLinkedMailContextDirective(t *testing.T) {
	local := newTestLocalCal(t)
	start := time.Now().Add(3 * time.Hour)
	ev, err := local.Create(localcal.CreateInput{
		Summary:     "ZTT 미팅",
		Start:       start,
		End:         start.Add(time.Hour),
		Source:      "mail:abc123",
		SourceLabel: "비금 154kV 통관",
		Kind:        "meeting",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{"action": "prep", "id": ev.ID})
	if !strings.Contains(out, "미팅 준비") || !strings.Contains(out, "mail:abc123") || !strings.Contains(out, "mail_archive") {
		t.Errorf("prep missing linked-context directive:\n%s", out)
	}
}

func TestCalendarPrepReturnsLinkedDocumentsSection(t *testing.T) {
	local := newTestLocalCal(t)
	start := time.Now().Add(3 * time.Hour)
	ev, err := local.Create(localcal.CreateInput{
		Summary: "ZTT 미팅", Start: start, End: start.Add(time.Hour),
		Source: "mail:abc", Kind: "meeting", Docs: []string{"ZTT_견적서.pdf"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{"action": "prep", "id": ev.ID})
	if !strings.Contains(out, "관련 문서") || !strings.Contains(out, "ZTT_견적서.pdf") || !strings.Contains(out, "files") {
		t.Errorf("prep should surface linked documents:\n%s", out)
	}
}

func TestCalendarPrepIgnoresAllDayWhenPickingNextMeeting(t *testing.T) {
	local := newTestLocalCal(t)
	loc := calDisplayLoc()
	now := time.Now().In(loc)
	// An all-day marker today (sorts first) + a timed meeting — "다음 미팅" should
	// be the meeting, not the all-day block.
	allDayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if _, err := local.Create(localcal.CreateInput{Summary: "휴가", Start: allDayStart, AllDay: true}); err != nil {
		t.Fatalf("seed all-day: %v", err)
	}
	timedStart := now.Add(2 * time.Hour)
	if _, err := local.Create(localcal.CreateInput{Summary: "ZTT 미팅", Start: timedStart, End: timedStart.Add(time.Hour)}); err != nil {
		t.Fatalf("seed timed: %v", err)
	}
	out := callCal(t, &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}, map[string]any{"action": "prep"})
	if !strings.Contains(out, "ZTT 미팅") || strings.Contains(out, "휴가") {
		t.Errorf("prep should target the next timed meeting, not the all-day block:\n%s", out)
	}
}
