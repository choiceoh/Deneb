package runstate

import (
	"context"
	"testing"
	"time"
)

func idleTestEntry(sessionKey string) *AbortEntry {
	return &AbortEntry{
		SessionKey: sessionKey,
		CancelFn:   func(error) {},
		ExpiresAt:  time.Now().Add(time.Hour),
	}
}

func TestSessionIdleWaitReturnsClosedChannelWhenAlreadyIdle(t *testing.T) {
	at := NewAbortTracker()
	select {
	case <-at.SessionIdleWait("client:main"):
	default:
		t.Fatal("idle session must hand back an already-closed wait")
	}
}

func TestSessionIdleWaitClosesWhenLastRunLeaves(t *testing.T) {
	at := NewAbortTracker()
	if !at.TryRegister("run-1", idleTestEntry("client:main")) {
		t.Fatal("register run-1")
	}
	if !at.TryRegister("run-2", idleTestEntry("client:main")) {
		t.Fatal("register run-2")
	}
	other := at.SessionIdleWait("client:other")
	wait := at.SessionIdleWait("client:main")
	select {
	case <-wait:
		t.Fatal("busy session must not be reported idle")
	default:
	}

	at.Cleanup("run-1")
	select {
	case <-wait:
		t.Fatal("one of two runs finishing is not idle")
	default:
	}

	at.Cleanup("run-2")
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("last run leaving must release the wait")
	}
	select {
	case <-other:
	default:
		t.Fatal("an unrelated idle session stays closed")
	}
}

// A run that dies without Cleanup is reclaimed by the expiry sweep; the ingress
// wait must be released with it or the session's queue would wedge forever.
func TestSessionIdleWaitReleasedByExpirySweep(t *testing.T) {
	at := NewAbortTracker()
	entry := idleTestEntry("client:main")
	entry.ExpiresAt = time.Now().Add(-time.Minute)
	if !at.TryRegister("run-dead", entry) {
		t.Fatal("register run-dead")
	}
	wait := at.SessionIdleWait("client:main")

	at.mu.Lock()
	now := time.Now()
	for id, tracked := range at.entries {
		if now.After(tracked.ExpiresAt) {
			delete(at.entries, id)
		}
	}
	at.signalIdleWaitersLocked()
	at.mu.Unlock()

	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("expired entry must release the ingress wait")
	}
}

func TestSessionIdleWaitReleasedByFatalDrain(t *testing.T) {
	at := NewAbortTracker()
	if !at.TryRegister("run-1", idleTestEntry("client:main")) {
		t.Fatal("register run-1")
	}
	wait := at.SessionIdleWait("client:main")
	at.FatalDrain(context.Canceled)
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("fatal drain must release ingress waits")
	}
}

// Cancel paths drop the entry outright, so a waiter they leave behind can no
// longer be rescued by Cleanup or the expiry sweep — both look for an entry that
// is already gone. Every removal path must release the wait itself.
func TestSessionIdleWaitReleasedByEveryCancelPath(t *testing.T) {
	cases := []struct {
		name   string
		cancel func(at *AbortTracker)
	}{
		{"InterruptSession", func(at *AbortTracker) { at.InterruptSession("client:main") }},
		{"CancelByRunID", func(at *AbortTracker) { at.CancelByRunID("run-1") }},
		{"CancelBySessionKey", func(at *AbortTracker) { at.CancelBySessionKey("client:main") }},
		{"Close", func(at *AbortTracker) { at.Close() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := NewAbortTracker()
			if !at.TryRegister("run-1", idleTestEntry("client:main")) {
				t.Fatal("register run-1")
			}
			wait := at.SessionIdleWait("client:main")
			select {
			case <-wait:
				t.Fatal("busy session must not be reported idle")
			default:
			}
			tc.cancel(at)
			select {
			case <-wait:
			case <-time.After(time.Second):
				t.Fatal("cancel path left the ingress wait blocked")
			}
		})
	}
}
