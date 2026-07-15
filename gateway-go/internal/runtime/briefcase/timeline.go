package briefcase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidTimeline    = errors.New("invalid briefcase timeline")
	ErrTimelineClockAhead = errors.New("briefcase timeline clock is ahead of next event")
	ErrTimelineBusy       = errors.New("briefcase timeline replay is already in progress")
)

// TimelineEvent is an immutable input event. Events are replayed by At, Order,
// then ID, so ordering does not depend on map iteration or input slice order.
type TimelineEvent struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Order   uint64    `json:"order,omitempty"`
	Kind    string    `json:"kind"`
	Payload rawJSON   `json:"payload,omitempty"`
}

type TimelineHandler func(context.Context, TimelineEvent) error

// Timeline replays a frozen event set against an explicit advancing clock.
type Timeline struct {
	mu      sync.Mutex
	clock   AdvancingClock
	events  []TimelineEvent
	cursor  int
	running bool
}

func NewTimeline(clock AdvancingClock, events []TimelineEvent) (*Timeline, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidTimeline)
	}
	start := canonicalTime(clock.Now())
	seen := make(map[string]struct{}, len(events))
	copyEvents := make([]TimelineEvent, len(events))
	for i, event := range events {
		event.ID = strings.TrimSpace(event.ID)
		event.Kind = strings.TrimSpace(event.Kind)
		event.At = canonicalTime(event.At)
		if event.ID == "" || event.Kind == "" || event.At.IsZero() {
			return nil, fmt.Errorf("%w: event %d needs id, kind, and non-zero time", ErrInvalidTimeline, i)
		}
		if _, ok := seen[event.ID]; ok {
			return nil, fmt.Errorf("%w: duplicate event id %q", ErrInvalidTimeline, event.ID)
		}
		seen[event.ID] = struct{}{}
		if event.At.Before(start) {
			return nil, fmt.Errorf("%w: event %q precedes clock start", ErrInvalidTimeline, event.ID)
		}
		payload, err := canonicalJSON(event.Payload)
		if err != nil {
			return nil, fmt.Errorf("%w: event %q payload: %w", ErrInvalidTimeline, event.ID, err)
		}
		event.Payload = payload
		copyEvents[i] = cloneTimelineEvent(event)
	}
	sort.Slice(copyEvents, func(i, j int) bool {
		if !copyEvents[i].At.Equal(copyEvents[j].At) {
			return copyEvents[i].At.Before(copyEvents[j].At)
		}
		if copyEvents[i].Order != copyEvents[j].Order {
			return copyEvents[i].Order < copyEvents[j].Order
		}
		return copyEvents[i].ID < copyEvents[j].ID
	})
	return &Timeline{clock: clock, events: copyEvents}, nil
}

// Step applies one event. The cursor advances only after the handler succeeds,
// so a failed handler may be retried with the same event and timestamp.
func (t *Timeline) Step(ctx context.Context, handler TimelineHandler) (bool, error) {
	if t == nil || handler == nil {
		return false, fmt.Errorf("%w: timeline and handler are required", ErrInvalidTimeline)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return false, ErrTimelineBusy
	}
	if t.cursor >= len(t.events) {
		t.mu.Unlock()
		return false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		t.mu.Unlock()
		return false, err
	}
	event := cloneTimelineEvent(t.events[t.cursor])
	now := canonicalTime(t.clock.Now())
	if now.After(event.At) {
		t.mu.Unlock()
		return false, fmt.Errorf("%w: now=%s next=%s event=%s", ErrTimelineClockAhead, now.Format(time.RFC3339Nano), event.At.Format(time.RFC3339Nano), event.ID)
	}
	if err := t.clock.AdvanceTo(event.At); err != nil {
		t.mu.Unlock()
		return false, fmt.Errorf("briefcase: advance timeline to event %q: %w", event.ID, err)
	}
	t.running = true
	t.mu.Unlock()

	if err := handler(ctx, event); err != nil {
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
		return false, fmt.Errorf("briefcase: replay event %q: %w", event.ID, err)
	}
	t.mu.Lock()
	t.cursor++
	t.running = false
	ctxErr := ctx.Err()
	t.mu.Unlock()
	if ctxErr != nil {
		// The handler returned success, so its side effects are already applied.
		// Commit the cursor before surfacing cancellation to preserve at-most-once
		// replay semantics.
		return true, fmt.Errorf("briefcase: replay event %q: %w", event.ID, ctxErr)
	}
	return true, nil
}

// ReplayUntil applies every event at or before until, then advances the clock to
// until. It never crosses a failed event.
func (t *Timeline) ReplayUntil(ctx context.Context, until time.Time, handler TimelineHandler) error {
	if t == nil || handler == nil {
		return fmt.Errorf("%w: timeline and handler are required", ErrInvalidTimeline)
	}
	until = canonicalTime(until)
	if until.Before(canonicalTime(t.clock.Now())) {
		return ErrClockRewind
	}
	for {
		t.mu.Lock()
		due := t.cursor < len(t.events) && !t.events[t.cursor].At.After(until)
		t.mu.Unlock()
		if !due {
			break
		}
		if _, err := t.Step(ctx, handler); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.clock.AdvanceTo(until); err != nil {
		return fmt.Errorf("briefcase: advance timeline to boundary: %w", err)
	}
	return nil
}

func (t *Timeline) ReplayAll(ctx context.Context, handler TimelineHandler) error {
	for {
		applied, err := t.Step(ctx, handler)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
	}
}

func (t *Timeline) Remaining() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events) - t.cursor
}

func (t *Timeline) Events() []TimelineEvent {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TimelineEvent, len(t.events))
	for i, event := range t.events {
		out[i] = cloneTimelineEvent(event)
	}
	return out
}

func cloneTimelineEvent(event TimelineEvent) TimelineEvent {
	event.Payload = bytes.Clone(event.Payload)
	return event
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("multiple JSON values")
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return out, nil
}
