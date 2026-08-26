package schedule

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// list marks a clash with ⚠️ and audit exists to hunt double-bookings down, but
// the write path used to report a clean success and say nothing — so booking
// onto an occupied slot looked identical to booking onto a free one.
func TestCalendarCreateWarnsAboutAnExistingEventInTheSameSlot(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)

	first := callCal(t, d, map[string]any{
		"action": "create", "summary": "선약 미팅", "start": start.Format(time.RFC3339),
	})
	if strings.Contains(first, "⚠️") {
		t.Fatalf("first event on an empty calendar reported a clash:\n%s", first)
	}

	second := callCal(t, d, map[string]any{
		"action": "create", "summary": "겹치는 미팅", "start": start.Format(time.RFC3339),
	})
	if !strings.Contains(second, "추가했습니다") {
		t.Fatalf("the write must still succeed — the operator asked for it:\n%s", second)
	}
	if !strings.Contains(second, "⚠️") || !strings.Contains(second, "선약 미팅") {
		t.Errorf("create did not name the event it collided with:\n%s", second)
	}
}

// Rescheduling onto an occupied slot is the same collision, reached the other
// way round.
func TestCalendarUpdateWarnsWhenRescheduledOntoAnOccupiedSlot(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	occupied := time.Now().Add(72 * time.Hour).Truncate(time.Hour)
	free := occupied.Add(4 * time.Hour)

	callCal(t, d, map[string]any{
		"action": "create", "summary": "선약 미팅", "start": occupied.Format(time.RFC3339),
	})
	moving := callCal(t, d, map[string]any{
		"action": "create", "summary": "옮길 미팅", "start": free.Format(time.RFC3339),
	})
	if strings.Contains(moving, "⚠️") {
		t.Fatalf("a free slot reported a clash:\n%s", moving)
	}

	moved := callCal(t, d, map[string]any{
		"action": "update", "id": extractCalID(moving), "summary": "옮길 미팅",
		"start": occupied.Format(time.RFC3339),
	})
	if !strings.Contains(moved, "⚠️") || !strings.Contains(moved, "선약 미팅") {
		t.Errorf("update onto an occupied slot did not warn:\n%s", moved)
	}
	// The event must not report colliding with itself.
	if strings.Count(moved, "옮길 미팅") > 1 {
		t.Errorf("event reported a clash with itself:\n%s", moved)
	}
}

// An all-day event spans the whole day by design; treating it as a clash with
// every meeting that day would make the warning worthless.
func TestCalendarCreateIgnoresAllDayEventsAsClashes(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	day := time.Now().Add(96 * time.Hour).Truncate(24 * time.Hour)

	callCal(t, d, map[string]any{
		"action": "create", "summary": "종일 워크숍", "start": day.Format(time.RFC3339),
		"all_day": true,
	})
	out := callCal(t, d, map[string]any{
		"action": "create", "summary": "그날 오후 미팅",
		"start": day.Add(15 * time.Hour).Format(time.RFC3339),
	})
	if strings.Contains(out, "⚠️") {
		t.Errorf("an all-day event was reported as a slot clash:\n%s", out)
	}
}

// A back-to-back meeting touches the previous one's end instant but does not
// overlap it.
func TestCalendarCreateDoesNotWarnForBackToBackMeetings(t *testing.T) {
	local := newTestLocalCal(t)
	d := &tooldeps.CalendarDeps{Local: wrapTestLocalCal(local)}
	start := time.Now().Add(120 * time.Hour).Truncate(time.Hour)

	callCal(t, d, map[string]any{
		"action": "create", "summary": "앞 미팅", "start": start.Format(time.RFC3339),
		"end": start.Add(time.Hour).Format(time.RFC3339),
	})
	out := callCal(t, d, map[string]any{
		"action": "create", "summary": "뒤 미팅",
		"start": start.Add(time.Hour).Format(time.RFC3339),
	})
	if strings.Contains(out, "⚠️") {
		t.Errorf("back-to-back meetings were reported as overlapping:\n%s", out)
	}
}
