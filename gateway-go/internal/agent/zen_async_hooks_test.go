package agent

import (
	"sync"
	"testing"
	"time"
)

func TestAsyncHookDispatcher_OrderPreservation(t *testing.T) {
	var mu sync.Mutex
	var received []string

	inner := StreamHooks{
		OnTextDelta: func(text string) {
			mu.Lock()
			received = append(received, text)
			mu.Unlock()
		},
	}

	d, proxy := NewAsyncHookDispatcher(inner)

	// Enqueue events rapidly.
	for i := 0; i < 10; i++ {
		proxy.OnTextDelta(string(rune('a' + i)))
	}

	d.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 10 {
		t.Fatalf("expected 10 events, got %d", len(received))
	}
	for i, s := range received {
		expected := string(rune('a' + i))
		if s != expected {
			t.Errorf("event %d: got %q, want %q", i, s, expected)
		}
	}
}

func TestAsyncHookDispatcher_NonBlocking(t *testing.T) {
	// Simulate a slow hook — verify that enqueue doesn't block.
	inner := StreamHooks{
		OnTextDelta: func(text string) {
			time.Sleep(10 * time.Millisecond) // slow hook
		},
	}

	d, proxy := NewAsyncHookDispatcher(inner)

	start := time.Now()
	// Enqueue several events — should not block because queue absorbs them.
	for i := 0; i < 10; i++ {
		proxy.OnTextDelta("delta")
	}
	enqueueTime := time.Since(start)

	d.Close()

	// Enqueue should have been near-instant (not 100ms+ for 10 slow hooks).
	if enqueueTime > 20*time.Millisecond {
		t.Fatalf("enqueue took %v, expected <20ms (hooks should be async)", enqueueTime)
	}
}

func TestAsyncHookDispatcher_AllHookTypes(t *testing.T) {
	var mu sync.Mutex
	var events []string

	inner := StreamHooks{
		OnTextDelta: func(text string) {
			mu.Lock()
			events = append(events, "delta:"+text)
			mu.Unlock()
		},
		OnThinking: func() {
			mu.Lock()
			events = append(events, "thinking")
			mu.Unlock()
		},
		OnToolStart: func(name, reason string) {
			mu.Lock()
			events = append(events, "start:"+name)
			mu.Unlock()
		},
		OnToolEmit: func(name, id string) {
			mu.Lock()
			events = append(events, "emit:"+name)
			mu.Unlock()
		},
		OnToolResult: func(name, id, result string, isErr bool) {
			mu.Lock()
			events = append(events, "result:"+name)
			mu.Unlock()
		},
	}

	d, proxy := NewAsyncHookDispatcher(inner)

	proxy.OnTextDelta("hello")
	proxy.OnThinking()
	proxy.OnToolStart("exec", "analyzing code")
	proxy.OnToolEmit("exec", "tool-1")
	proxy.OnToolResult("exec", "tool-1", "ok", false)

	d.Close()

	mu.Lock()
	defer mu.Unlock()

	expected := []string{"delta:hello", "thinking", "start:exec", "emit:exec", "result:exec"}
	if len(events) != len(expected) {
		t.Fatalf("got %d events, want %d: %v", len(events), len(expected), events)
	}
	for i, e := range events {
		if e != expected[i] {
			t.Errorf("event %d: got %q, want %q", i, e, expected[i])
		}
	}
}

func TestAsyncHookDispatcher_NilHooksSkipped(t *testing.T) {
	// No hooks set — should not panic.
	d, proxy := NewAsyncHookDispatcher(StreamHooks{})

	// These should all be nil and not enqueue.
	if proxy.OnTextDelta != nil {
		t.Fatal("expected nil OnTextDelta proxy for nil inner hook")
	}
	if proxy.OnThinking != nil {
		t.Fatal("expected nil OnThinking proxy for nil inner hook")
	}

	d.Close()
}
