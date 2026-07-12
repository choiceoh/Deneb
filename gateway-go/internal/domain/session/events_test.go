package session

import (
	"sync"
	"testing"
	"time"
)

// waitFor polls fn until it returns true or the deadline fires.
// Used by tests to await async event delivery without flaky time.Sleep.
func waitFor(t *testing.T, fn func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestEventBusSubscribeEmit(t *testing.T) {
	bus := NewEventBus()

	var received []Event
	var mu sync.Mutex

	bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	bus.Emit(Event{Kind: EventCreated, Key: "sess-1"})
	bus.Emit(Event{Kind: EventStatusChanged, Key: "sess-1", OldStatus: "", NewStatus: StatusRunning})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 2
	}, time.Second, "2 events")

	mu.Lock()
	defer mu.Unlock()
	if received[0].Kind != EventCreated {
		t.Errorf("got %s, want EventCreated", received[0].Kind)
	}
	if received[1].NewStatus != StatusRunning {
		t.Errorf("got %s, want StatusRunning", received[1].NewStatus)
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	var count int
	var mu sync.Mutex
	unsub := bus.Subscribe(func(_ Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus.Emit(Event{Kind: EventCreated, Key: "s1"})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return count == 1
	}, time.Second, "first event delivered")

	unsub()
	bus.Emit(Event{Kind: EventDeleted, Key: "s1"})

	// Give any lingering dispatch a chance to run; then assert count unchanged.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("got %d, want still 1 after unsub", count)
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	var count1, count2 int
	var mu sync.Mutex

	bus.Subscribe(func(_ Event) {
		mu.Lock()
		count1++
		mu.Unlock()
	})
	bus.Subscribe(func(_ Event) {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	bus.Emit(Event{Kind: EventCreated, Key: "s1"})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return count1 == 1 && count2 == 1
	}, time.Second, "both subscribers receive event")
}
