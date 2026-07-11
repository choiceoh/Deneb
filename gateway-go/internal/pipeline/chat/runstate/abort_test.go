package runstate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func abortEntry(session, run string) (*AbortEntry, context.Context) {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &AbortEntry{
		SessionKey: session,
		ClientRun:  run,
		CancelFn:   cancel,
		ExpiresAt:  time.Now().Add(time.Hour),
	}, ctx
}

func TestAbortTrackerSessionLifecycle(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	first, firstCtx := abortEntry("client:one", "run-1")
	second, secondCtx := abortEntry("client:one", "run-2")
	other, otherCtx := abortEntry("client:two", "run-3")
	tracker.Register(first.ClientRun, first)
	tracker.Register(second.ClientRun, second)
	tracker.Register(other.ClientRun, other)

	if got := tracker.CountForSession("client:one"); got != 2 {
		t.Fatalf("CountForSession(client:one) = %d, want 2", got)
	}
	if !tracker.HasActiveRun("client:two") {
		t.Fatal("HasActiveRun(client:two) = false, want true")
	}

	cause := errors.New("superseded turn")
	if got := tracker.CancelBySessionKeyWithCause("client:one", cause); got == "" {
		t.Fatal("CancelBySessionKeyWithCause returned an empty run id")
	}
	if !errors.Is(context.Cause(firstCtx), cause) || !errors.Is(context.Cause(secondCtx), cause) {
		t.Fatalf("session cancellation causes = (%v, %v), want %v", context.Cause(firstCtx), context.Cause(secondCtx), cause)
	}
	if context.Cause(otherCtx) != nil {
		t.Fatalf("unrelated run was cancelled: %v", context.Cause(otherCtx))
	}
	if tracker.HasActiveRun("client:one") {
		t.Fatal("cancelled session remains active")
	}
}

func TestAbortTrackerCancelByRunIDAndCleanup(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	entry, ctx := abortEntry("client:main", "run-1")
	tracker.Register("run-1", entry)
	session, run := tracker.CancelByRunID("run-1")
	if session != "client:main" || run != "run-1" {
		t.Fatalf("CancelByRunID = (%q, %q)", session, run)
	}
	if context.Cause(ctx) != context.Canceled {
		t.Fatalf("cancellation cause = %v, want context.Canceled", context.Cause(ctx))
	}
	if session, run = tracker.CancelByRunID("run-1"); session != "" || run != "" {
		t.Fatalf("second CancelByRunID = (%q, %q), want empty", session, run)
	}

	entry, ctx = abortEntry("client:main", "run-2")
	tracker.Register("run-2", entry)
	tracker.Cleanup("run-2")
	if tracker.HasActiveRun("client:main") {
		t.Fatal("Cleanup left the run active")
	}
	if context.Cause(ctx) != nil {
		t.Fatalf("Cleanup should not cancel a completed run: %v", context.Cause(ctx))
	}
}

func TestAbortTrackerConcurrentRegisterAndInterrupt(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	const runs = 64
	contexts := make([]context.Context, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entry, ctx := abortEntry("shared", fmt.Sprintf("run-%d", i))
			contexts[i] = ctx
			tracker.Register(entry.ClientRun, entry)
		}(i)
	}
	wg.Wait()
	if got := tracker.CountForSession("shared"); got != runs {
		t.Fatalf("registered runs = %d, want %d", got, runs)
	}

	tracker.InterruptSession("shared")
	if tracker.HasActiveRun("shared") {
		t.Fatal("InterruptSession left active runs")
	}
	for i, ctx := range contexts {
		if context.Cause(ctx) != context.Canceled {
			t.Errorf("run %d cause = %v, want context.Canceled", i, context.Cause(ctx))
		}
	}
}

func TestAbortTrackerCloseIsIdempotent(t *testing.T) {
	tracker := NewAbortTracker()
	entry, ctx := abortEntry("client:main", "run")
	tracker.Register("run", entry)

	tracker.Close()
	tracker.Close()
	if context.Cause(ctx) != context.Canceled {
		t.Fatalf("Close cancellation cause = %v", context.Cause(ctx))
	}
	if tracker.CountForSession("client:main") != 0 {
		t.Fatal("Close did not clear entries")
	}
}
