package session

import (
	"sync"
	"testing"
)

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

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	if received[0].Kind != EventCreated {
		t.Errorf("expected EventCreated, got %s", received[0].Kind)
	}
	if received[1].NewStatus != StatusRunning {
		t.Errorf("expected StatusRunning, got %s", received[1].NewStatus)
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	count := 0
	unsub := bus.Subscribe(func(_ Event) {
		count++
	})

	bus.Emit(Event{Kind: EventCreated, Key: "s1"})
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}

	unsub()
	bus.Emit(Event{Kind: EventDeleted, Key: "s1"})
	if count != 1 {
		t.Fatalf("expected still 1 after unsub, got %d", count)
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	var count1, count2 int

	bus.Subscribe(func(_ Event) { count1++ })
	bus.Subscribe(func(_ Event) { count2++ })

	bus.Emit(Event{Kind: EventCreated, Key: "s1"})

	if count1 != 1 || count2 != 1 {
		t.Errorf("expected both to receive: count1=%d count2=%d", count1, count2)
	}
}
