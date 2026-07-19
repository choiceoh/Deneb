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

func TestAbortTrackerCancelsAllRunsForSession(t *testing.T) {
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
	if !errors.Is(context.Cause(ctx), context.Canceled) {
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
		if !errors.Is(context.Cause(ctx), context.Canceled) {
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
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatalf("Close cancellation cause = %v", context.Cause(ctx))
	}
	if tracker.CountForSession("client:main") != 0 {
		t.Fatal("Close did not clear entries")
	}
}

func TestAbortTrackerDrainRejectsNewRunsAndWaitsForContinuations(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	active, _ := abortEntry("client:main", "run-active")
	if !tracker.TryRegister(active.ClientRun, active) {
		t.Fatal("initial run was rejected before draining")
	}
	drained := tracker.BeginDrain()

	newRun, _ := abortEntry("client:new", "run-new")
	if tracker.TryRegister(newRun.ClientRun, newRun) {
		t.Fatal("new top-level run was admitted after draining began")
	}
	continuation, _ := abortEntry("client:main", "run-queued")
	if !tracker.RegisterContinuation(continuation.ClientRun, continuation) {
		t.Fatal("already-queued continuation was rejected while its parent was active")
	}

	tracker.Cleanup(active.ClientRun)
	select {
	case <-drained:
		t.Fatal("drain completed while an accepted continuation was still active")
	default:
	}

	tracker.Cleanup(continuation.ClientRun)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain did not complete after the final accepted run cleaned up")
	}

	lateContinuation, _ := abortEntry("client:main", "run-late")
	if tracker.RegisterContinuation(lateContinuation.ClientRun, lateContinuation) {
		t.Fatal("continuation reopened admission after idle was observed")
	}
}

func TestAbortTrackerDrainWaitsForCancelledRunCleanup(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	entry, _ := abortEntry("client:main", "run-cancelled")
	tracker.Register(entry.ClientRun, entry)
	drained := tracker.BeginDrain()
	tracker.CancelByRunID(entry.ClientRun)

	select {
	case <-drained:
		t.Fatal("cancellation released drain before the run goroutine cleaned up")
	default:
	}
	tracker.Cleanup(entry.ClientRun)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not release drain after cancellation")
	}
}

func TestAbortTrackerDrainWaitsForPreDrainAdmissionToRegister(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	if !tracker.AcquireAdmission() {
		t.Fatal("request admission was rejected before draining")
	}
	drained := tracker.BeginDrain()
	select {
	case <-drained:
		t.Fatal("drain completed while a pre-drain admission was still reserved")
	default:
	}

	entry, _ := abortEntry("client:main", "run-admitted")
	if !tracker.RegisterAdmitted(entry.ClientRun, entry) {
		t.Fatal("pre-drain admission could not register after draining began")
	}
	tracker.ReleaseAdmission()
	select {
	case <-drained:
		t.Fatal("drain completed while the admitted run was still active")
	default:
	}

	tracker.Cleanup(entry.ClientRun)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain did not complete after admitted run cleanup")
	}
}

func TestAbortTrackerHasOtherActiveRunIgnoresFinishingRun(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	current, _ := abortEntry("client:main", "run-current")
	other, _ := abortEntry("client:main", "run-other")
	unrelated, _ := abortEntry("client:other", "run-unrelated")
	tracker.Register(current.ClientRun, current)
	tracker.Register(unrelated.ClientRun, unrelated)

	if tracker.HasOtherActiveRun("client:main", current.ClientRun) {
		t.Fatal("unrelated session was mistaken for a successor")
	}
	tracker.Register(other.ClientRun, other)
	if !tracker.HasOtherActiveRun("client:main", current.ClientRun) {
		t.Fatal("same-session successor was not detected")
	}
	tracker.Cleanup(other.ClientRun)
	if tracker.HasOtherActiveRun("client:main", current.ClientRun) {
		t.Fatal("cleaned-up successor still reported active")
	}
}

// The /health activity contract: the deploy idle gate reads the exact
// population the drain waits for — admissions count until registered, active
// runs count until cleanup, and an idle tracker reads zero.
func TestActiveRunCountTracksAdmissionsAndRuns(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	if got := tracker.ActiveRunCount(); got != 0 {
		t.Fatalf("idle ActiveRunCount = %d, want 0", got)
	}

	if !tracker.AcquireAdmission() {
		t.Fatal("admission rejected on idle tracker")
	}
	if got := tracker.ActiveRunCount(); got != 1 {
		t.Fatalf("ActiveRunCount after admission = %d, want 1", got)
	}

	entry, _ := abortEntry("client:main", "run-counted")
	if !tracker.RegisterAdmitted(entry.ClientRun, entry) {
		t.Fatal("admitted run could not register")
	}
	tracker.ReleaseAdmission()
	if got := tracker.ActiveRunCount(); got != 1 {
		t.Fatalf("ActiveRunCount after register = %d, want 1 (admission handed off to run)", got)
	}

	tracker.Cleanup(entry.ClientRun)
	if got := tracker.ActiveRunCount(); got != 0 {
		t.Fatalf("ActiveRunCount after cleanup = %d, want 0", got)
	}
}

// HasActiveInteractiveRun ignores automation entries: an autonomous relay
// (heartbeat/cron) riding a session key the user chats on must not make the
// session look busy to auto-steer. Regression for the 2026-07-18 report where
// a user's message folded into a heartbeat run on client:main.
func TestHasActiveInteractiveRunIgnoresAutomation(t *testing.T) {
	tracker := NewAbortTracker()
	t.Cleanup(tracker.Close)

	auto, _ := abortEntry("client:main", "hb-1")
	auto.Automation = true
	tracker.Register(auto.ClientRun, auto)

	// Only an automation run is active → NOT interactive-active.
	if !tracker.HasActiveRun("client:main") {
		t.Fatal("HasActiveRun should still see the automation run")
	}
	if tracker.HasActiveInteractiveRun("client:main") {
		t.Fatal("automation-only session must not count as interactive-active")
	}

	// A concurrent interactive run flips it.
	inter, _ := abortEntry("client:main", "chat-1")
	tracker.Register(inter.ClientRun, inter)
	if !tracker.HasActiveInteractiveRun("client:main") {
		t.Fatal("interactive run must make the session interactive-active")
	}

	// Removing the interactive run drops it back to automation-only.
	tracker.Cleanup("chat-1")
	if tracker.HasActiveInteractiveRun("client:main") {
		t.Fatal("after the interactive run ends, automation alone is not interactive")
	}
}
