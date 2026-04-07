package chat

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

func TestNotifyQueue_SingleItem(t *testing.T) {
	var flushed []notifyItem
	var mu sync.Mutex
	done := make(chan struct{})

	q := &notifyQueue{
		capacity: 20,
		flushFn: func(items []notifyItem) {
			mu.Lock()
			flushed = items
			mu.Unlock()
			close(done)
		},
	}

	q.enqueue(notifyItem{childKey: "child:1", label: "worker-1", status: session.StatusDone})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("flush did not fire within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 item, got %d", len(flushed))
	}
	if flushed[0].label != "worker-1" {
		t.Errorf("expected label worker-1, got %s", flushed[0].label)
	}
}

func TestNotifyQueue_Batching(t *testing.T) {
	var flushed []notifyItem
	var mu sync.Mutex
	done := make(chan struct{})

	q := &notifyQueue{
		capacity: 20,
		flushFn: func(items []notifyItem) {
			mu.Lock()
			flushed = items
			mu.Unlock()
			close(done)
		},
	}

	// Enqueue 3 items rapidly — should batch into a single flush.
	q.enqueue(notifyItem{childKey: "child:1", label: "a", status: session.StatusDone})
	q.enqueue(notifyItem{childKey: "child:2", label: "b", status: session.StatusFailed})
	q.enqueue(notifyItem{childKey: "child:3", label: "c", status: session.StatusDone})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("flush did not fire within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 3 {
		t.Fatalf("expected 3 items batched, got %d", len(flushed))
	}
}

func TestNotifyQueue_DebounceResets(t *testing.T) {
	var flushCount int
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	q := &notifyQueue{
		capacity: 20,
		flushFn: func(items []notifyItem) {
			mu.Lock()
			flushCount++
			mu.Unlock()
			select {
			case done <- struct{}{}:
			default:
			}
		},
	}

	// First enqueue.
	q.enqueue(notifyItem{childKey: "child:1", label: "a", status: session.StatusDone})

	// Wait 500ms (less than debounce), enqueue another.
	time.Sleep(500 * time.Millisecond)
	q.enqueue(notifyItem{childKey: "child:2", label: "b", status: session.StatusDone})

	// Should get only one flush with both items.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("flush did not fire within timeout")
	}

	// Wait to ensure no extra flushes.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if flushCount != 1 {
		t.Errorf("expected 1 flush (debounced), got %d", flushCount)
	}
}

func TestFormatBatchNotification_Single(t *testing.T) {
	items := []notifyItem{
		{label: "worker-1", status: session.StatusDone, runtimeMs: 5000, lastOutput: "task completed"},
	}
	text := formatBatchNotification(items)

	if !strings.Contains(text, "**System:** subagent completed") {
		t.Error("single item should use singular header")
	}
	if !strings.Contains(text, "worker-1") {
		t.Error("should contain agent label")
	}
	if !strings.Contains(text, "task completed") {
		t.Error("should contain result text")
	}
}

func TestFormatBatchNotification_Multiple(t *testing.T) {
	items := []notifyItem{
		{label: "worker-1", status: session.StatusDone},
		{label: "worker-2", status: session.StatusFailed, failureReason: "timeout"},
	}
	text := formatBatchNotification(items)

	if !strings.Contains(text, "2 subagents completed") {
		t.Error("batch should use plural header")
	}
	if !strings.Contains(text, "worker-1") || !strings.Contains(text, "worker-2") {
		t.Error("should contain all agent labels")
	}
	if !strings.Contains(text, "timeout") {
		t.Error("should contain failure reason")
	}
}

func TestFormatBatchNotification_Overflow(t *testing.T) {
	items := make([]notifyItem, 25)
	for i := range items {
		items[i] = notifyItem{label: "w", status: session.StatusDone}
	}
	text := formatBatchNotification(items)

	if !strings.Contains(text, "5 more") {
		t.Errorf("overflow should mention remaining count, got: %s", text)
	}
}

func TestFormatBatchNotification_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 3000)
	items := []notifyItem{
		{label: "w", status: session.StatusDone, lastOutput: longOutput},
	}
	text := formatBatchNotification(items)

	if !strings.Contains(text, "truncated") {
		t.Error("long output should be truncated")
	}
	if len(text) > 2500 {
		// The formatted text should be capped near maxOutputLen (2000) + headers.
		t.Errorf("formatted text too long: %d chars", len(text))
	}
}

func TestIsTerminalStatus(t *testing.T) {
	for _, s := range []session.RunStatus{session.StatusDone, session.StatusFailed, session.StatusKilled, session.StatusTimeout} {
		if !isTerminalStatus(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	if isTerminalStatus(session.StatusRunning) {
		t.Error("running should not be terminal")
	}
	if isTerminalStatus("") {
		t.Error("empty status should not be terminal")
	}
}

func TestDeferredSubagentNotifications_Nil(t *testing.T) {
	fn := deferredSubagentNotifications(nil)
	if fn != nil {
		t.Error("should return nil when channel is nil")
	}
}

func TestDeferredSubagentNotifications_Drain(t *testing.T) {
	ch := make(chan string, 2)
	ch <- "child A done"
	ch <- "child B done"

	fn := deferredSubagentNotifications(ch)
	result := fn()

	// Should drain both notifications.
	if !strings.Contains(result, "child A done") || !strings.Contains(result, "child B done") {
		t.Errorf("should contain both notifications, got %q", result)
	}

	// Channel empty — should return empty.
	result = fn()
	if result != "" {
		t.Errorf("expected empty when channel drained, got %q", result)
	}
}
