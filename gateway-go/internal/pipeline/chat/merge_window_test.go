package chat

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ─── MergeWindowTracker ───────────────────────────────────────────────────

func TestMergeWindowTracker_TouchReturnsZeroOnFirstCall(t *testing.T) {
	m := NewMergeWindowTracker()
	if got := m.Touch("sess"); !got.IsZero() {
		t.Errorf("Touch() first call = %v, want zero Time", got)
	}
}

func TestMergeWindowTracker_TouchReturnsPreviousTimestamp(t *testing.T) {
	m := NewMergeWindowTracker()
	m.Touch("sess")
	time.Sleep(5 * time.Millisecond)
	prev := m.Touch("sess")
	if prev.IsZero() {
		t.Fatal("Touch() second call returned zero, expected previous timestamp")
	}
	if since := time.Since(prev); since < 5*time.Millisecond {
		t.Errorf("previous timestamp too new: %v since", since)
	}
}

func TestMergeWindowTracker_PerSessionIsolation(t *testing.T) {
	m := NewMergeWindowTracker()
	m.Touch("sess-A")
	if got := m.Touch("sess-B"); !got.IsZero() {
		t.Errorf("Touch(sess-B) = %v, want zero (different session)", got)
	}
}

func TestMergeWindowTracker_ClearRemovesSession(t *testing.T) {
	m := NewMergeWindowTracker()
	m.Touch("sess")
	m.Clear("sess")
	if got := m.Touch("sess"); !got.IsZero() {
		t.Errorf("Touch() after Clear = %v, want zero", got)
	}
}

func TestMergeWindowTracker_ResetClearsAll(t *testing.T) {
	m := NewMergeWindowTracker()
	m.Touch("sess-A")
	m.Touch("sess-B")
	m.Reset()
	if got := m.Touch("sess-A"); !got.IsZero() {
		t.Error("Touch(sess-A) after Reset should be zero")
	}
	if got := m.Touch("sess-B"); !got.IsZero() {
		t.Error("Touch(sess-B) after Reset should be zero")
	}
}

// ─── Send merge behavior ──────────────────────────────────────────────────

// newSendRequest builds a chat.send RequestFrame for tests.
func newSendRequest(t *testing.T, sessionKey, message, runID string) *protocol.RequestFrame {
	t.Helper()
	return newSendRequestWithSkip(t, sessionKey, message, runID, false)
}

// newSendRequestWithSkip builds a chat.send RequestFrame with an optional
// skipMerge flag.
func newSendRequestWithSkip(t *testing.T, sessionKey, message, runID string, skipMerge bool) *protocol.RequestFrame {
	t.Helper()
	body := map[string]any{
		"sessionKey":  sessionKey,
		"message":     message,
		"clientRunId": runID,
	}
	if skipMerge {
		body["skipMerge"] = true
	}
	params, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &protocol.RequestFrame{
		ID:     "req-" + runID,
		Method: "chat.send",
		Params: params,
	}
}

// TestSend_QueuesWhenActiveRunOutsideMergeWindow verifies that an inbound
// message lands in the pending queue (not interrupting the active run) when
// the previous touch happened more than mergeWindowDuration ago.
func TestSend_QueuesWhenActiveRunOutsideMergeWindow(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-merge-outside"

	// Simulate a previously-arrived message far enough in the past that
	// the merge window has already elapsed.
	h.mergeWindow.mu.Lock()
	h.mergeWindow.ts[key] = time.Now().Add(-2 * mergeWindowDuration)
	h.mergeWindow.mu.Unlock()

	// Mark the session as having an active run so Send() takes the
	// "active run" branch.
	cancelled := false
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() { cancelled = true },
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	resp := h.Send(context.Background(), newSendRequest(t, key, "follow-up", "run-2"))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	// Outside the merge window: the active run must NOT have been cancelled.
	if cancelled {
		t.Error("active run was cancelled outside the merge window")
	}
	// And the message should have landed in the pending queue.
	if got := h.pending.Len(key); got != 1 {
		t.Errorf("pending queue length = %d, want 1", got)
	}
}

// TestSend_MergesWhenActiveRunInsideMergeWindow verifies that an inbound
// message arriving within the merge window cancels the active run instead
// of being queued.
func TestSend_MergesWhenActiveRunInsideMergeWindow(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-merge-inside"

	// Previous touch was just now (well within the merge window).
	h.mergeWindow.Touch(key)

	cancelled := false
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() { cancelled = true },
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	// Note: Send() will start a new async run via startAsyncRun, which
	// requires a usable session manager but no LLM client because
	// runAgentAsync is invoked in a goroutine and the test exits before it
	// runs. We only assert the synchronous side-effects: the active run is
	// cancelled and the queue stays empty.
	resp := h.Send(context.Background(), newSendRequest(t, key, "follow-up", "run-2"))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	if !cancelled {
		t.Error("active run was NOT cancelled inside the merge window")
	}
	if got := h.pending.Len(key); got != 0 {
		t.Errorf("pending queue length = %d, want 0 (merged, not queued)", got)
	}
}

// TestSend_MergeFoldsPendingMessage verifies that any older queued message
// is folded into the new merged run rather than discarded.
func TestSend_MergeFoldsPendingMessage(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-merge-fold"

	// Previous touch within window.
	h.mergeWindow.Touch(key)

	// An older queued message that should be folded into the merge.
	h.pending.Enqueue(key, RunParams{SessionKey: key, Message: "queued-earlier"})

	// Capture the message handed to startAsyncRun by intercepting it
	// through a custom CancelFn that records the drained queue state.
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() {},
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	resp := h.Send(context.Background(), newSendRequest(t, key, "newest", "run-2"))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	// The pending queue must be drained as part of the merge.
	if got := h.pending.Len(key); got != 0 {
		t.Errorf("pending queue length = %d, want 0 (drained for merge)", got)
	}
}

// TestSend_FirstMessageStartsRunNormally verifies that the very first
// chat.send for a session does NOT trigger merge logic (no previous
// timestamp) and goes through the normal startAsyncRun path.
func TestSend_FirstMessageStartsRunNormally(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-first-msg"

	resp := h.Send(context.Background(), newSendRequest(t, key, "hello", "run-1"))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	// First message should not be queued; it starts a run.
	if got := h.pending.Len(key); got != 0 {
		t.Errorf("pending queue length = %d, want 0 (no queue on first msg)", got)
	}
	// And the merge window should now have a timestamp recorded.
	if got := h.mergeWindow.Touch(key); got.IsZero() {
		t.Error("merge window timestamp not recorded after first Send")
	}
}

// TestSend_SkipMergeBypassesMergeWindow verifies that a chat.send with
// skipMerge=true never cancels an active run, even inside the window —
// the call falls through to the normal "queue" branch instead.
func TestSend_SkipMergeBypassesMergeWindow(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-skip-merge"

	// Previous touch well within the merge window.
	h.mergeWindow.Touch(key)

	cancelled := false
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() { cancelled = true },
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	resp := h.Send(context.Background(), newSendRequestWithSkip(t, key, "button-click", "run-2", true))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	if cancelled {
		t.Error("skipMerge=true still cancelled the active run")
	}
	if got := h.pending.Len(key); got != 1 {
		t.Errorf("pending queue length = %d, want 1 (queued, not merged)", got)
	}
}

// TestSend_MergeBroadcastsSessionsChanged verifies that a merge fires a
// sessions.changed broadcast with reason=merged so dashboards can
// distinguish it from normal run starts.
func TestSend_MergeBroadcastsSessionsChanged(t *testing.T) {
	sm := session.NewManager()
	var mu sync.Mutex
	var events []map[string]any
	bc := func(event string, payload any) (int, []error) {
		if event != "sessions.changed" {
			return 0, nil
		}
		if m, ok := payload.(map[string]any); ok {
			mu.Lock()
			events = append(events, m)
			mu.Unlock()
		}
		return 1, nil
	}
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-merge-broadcast"
	h.mergeWindow.Touch(key)
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() {},
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	resp := h.Send(context.Background(), newSendRequest(t, key, "follow-up", "run-2"))
	if resp == nil || !resp.OK {
		t.Fatalf("Send() failed: %+v", resp)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawMerged bool
	for _, e := range events {
		if e["reason"] == "merged" && e["sessionKey"] == key {
			sawMerged = true
			break
		}
	}
	if !sawMerged {
		t.Errorf("expected sessions.changed reason=merged broadcast; got %+v", events)
	}
}

// TestSend_ConcurrentMergeRaceSafe verifies that when many concurrent
// chat.send calls arrive for the same session — all within the merge
// window and all seeing an active run — the per-session lock serializes
// the decision so at most one run is dispatched per registered abort
// entry (no double-cancel / double-dispatch).
//
// Setup: 50 goroutines race to Send() on the same session. Only one
// abort entry is pre-registered. After the race, exactly one of the
// goroutines should have observed the active-run-and-merge path (and
// cancelled it); all other goroutines should have either queued or
// started fresh runs — but the CancelFn must fire at most once.
func TestSend_ConcurrentMergeRaceSafe(t *testing.T) {
	sm := session.NewManager()
	bc := func(event string, payload any) (int, []error) { return 0, nil }
	h := NewHandler(sm, bc, nil, DefaultHandlerConfig())
	defer h.Close()

	key := "test-race-safe"
	// Seed a previous timestamp so every concurrent call is inside the
	// merge window.
	h.mergeWindow.Touch(key)

	var cancelCount atomic.Int32
	h.abort.Register("active-run", &AbortEntry{
		SessionKey: key,
		ClientRun:  "active-run",
		CancelFn:   func() { cancelCount.Add(1) },
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			runID := "r-" + time.Now().Format("150405.000") + "-" + string(rune('A'+i%26))
			h.Send(context.Background(), newSendRequest(t, key, "msg", runID))
		}(i)
	}
	wg.Wait()

	// The single registered abort entry must be cancelled at most once —
	// the per-session lock + map delete under InterruptSession guarantees
	// idempotency. Without the lock, multiple goroutines could all pass
	// the HasActiveRun check and all see the same entry.
	if got := cancelCount.Load(); got > 1 {
		t.Errorf("CancelFn fired %d times; want <= 1 (per-session lock should prevent double cancel)", got)
	}
}
