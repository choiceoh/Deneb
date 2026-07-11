package briefcase

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTimelineDeterministicReplayAndBoundaries(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	events := []TimelineEvent{
		{ID: "z", Kind: "mail", At: start.Add(2 * time.Hour), Payload: json.RawMessage(`{"v":4}`)},
		{ID: "b", Kind: "device", At: start.Add(time.Hour), Order: 2, Payload: json.RawMessage(`{"b":2,"a":1}`)},
		{ID: "c", Kind: "wiki", At: start.Add(time.Hour), Order: 1, Payload: json.RawMessage(`{"v":3}`)},
		{ID: "a", Kind: "mail", At: start.Add(time.Hour), Order: 1, Payload: json.RawMessage(`{"v":1}`)},
	}
	timeline, err := NewTimeline(clock, events)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating caller-owned input after construction must not alter the replay.
	events[0].ID = "mutated"

	var got []string
	err = timeline.ReplayUntil(context.Background(), start.Add(time.Hour), func(_ context.Context, event TimelineEvent) error {
		got = append(got, event.ID)
		// A handler may inspect the timeline without deadlocking replay.
		if timeline.Remaining() == 0 {
			t.Error("remaining unexpectedly zero inside handler")
		}
		if len(event.Payload) > 0 {
			event.Payload[0] = '!'
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "c", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if timeline.Remaining() != 1 {
		t.Fatalf("remaining = %d, want 1", timeline.Remaining())
	}
	if !clock.Now().Equal(start.Add(time.Hour)) {
		t.Fatalf("clock = %v, want replay boundary", clock.Now())
	}
	if payload := string(timeline.Events()[2].Payload); payload != `{"a":1,"b":2}` {
		t.Fatalf("canonical/copied payload = %s", payload)
	}
	if err := timeline.ReplayAll(context.Background(), func(_ context.Context, event TimelineEvent) error {
		got = append(got, event.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != "z" || !clock.Now().Equal(start.Add(2*time.Hour)) {
		t.Fatalf("final replay = %v at %v", got, clock.Now())
	}
}

func TestTimelineFailureDoesNotConsumeEvent(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	timeline, err := NewTimeline(clock, []TimelineEvent{{
		ID: "event", Kind: "mail", At: start.Add(time.Minute), Payload: json.RawMessage(`{"ok":true}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("handler failed")
	if _, err := timeline.Step(context.Background(), func(context.Context, TimelineEvent) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("Step error = %v, want handler failure", err)
	}
	if timeline.Remaining() != 1 {
		t.Fatalf("failed event was consumed, remaining=%d", timeline.Remaining())
	}
	if _, err := timeline.Step(context.Background(), func(context.Context, TimelineEvent) error { return nil }); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if timeline.Remaining() != 0 {
		t.Fatalf("retried event not consumed, remaining=%d", timeline.Remaining())
	}
}

func TestTimelineRejectsInvalidAndClockAheadState(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		events []TimelineEvent
	}{
		{name: "duplicate", events: []TimelineEvent{{ID: "x", Kind: "a", At: start}, {ID: "x", Kind: "b", At: start}}},
		{name: "past", events: []TimelineEvent{{ID: "x", Kind: "a", At: start.Add(-time.Second)}}},
		{name: "bad json", events: []TimelineEvent{{ID: "x", Kind: "a", At: start, Payload: json.RawMessage(`{"a":1} trailing`)}}},
		{name: "missing kind", events: []TimelineEvent{{ID: "x", At: start}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTimeline(NewManualClock(start), tc.events); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("error = %v, want ErrInvalidTimeline", err)
			}
		})
	}

	clock := NewManualClock(start)
	timeline, err := NewTimeline(clock, []TimelineEvent{{ID: "x", Kind: "mail", At: start.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := clock.Advance(2 * time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := timeline.Step(context.Background(), func(context.Context, TimelineEvent) error { return nil }); !errors.Is(err, ErrTimelineClockAhead) {
		t.Fatalf("error = %v, want ErrTimelineClockAhead", err)
	}
}

func TestTimelineCanceledContextDoesNotAdvance(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	timeline, err := NewTimeline(clock, []TimelineEvent{{ID: "x", Kind: "mail", At: start.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := timeline.Step(ctx, func(context.Context, TimelineEvent) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if !clock.Now().Equal(start) || timeline.Remaining() != 1 {
		t.Fatalf("canceled replay mutated state: now=%v remaining=%d", clock.Now(), timeline.Remaining())
	}
}

func TestTimelineRejectsReentrantStep(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	timeline, err := NewTimeline(NewManualClock(start), []TimelineEvent{{ID: "x", Kind: "mail", At: start}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = timeline.Step(context.Background(), func(context.Context, TimelineEvent) error {
		_, nestedErr := timeline.Step(context.Background(), func(context.Context, TimelineEvent) error { return nil })
		return nestedErr
	})
	if !errors.Is(err, ErrTimelineBusy) {
		t.Fatalf("reentrant error = %v, want ErrTimelineBusy", err)
	}
	if timeline.Remaining() != 1 {
		t.Fatalf("reentrant failure consumed event, remaining=%d", timeline.Remaining())
	}
}
