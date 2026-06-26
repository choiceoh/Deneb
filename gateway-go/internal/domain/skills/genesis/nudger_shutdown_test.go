package genesis

import (
	"context"
	"testing"
)

// A nudger with no shutdown context wired falls back to context.Background(),
// preserving the prior (never-cancelled-by-us) behavior.
func TestNudger_BaseContextDefaultsToBackground(t *testing.T) {
	n := NewNudger(nil, NudgerConfig{}, nil)
	ctx := n.baseContext()
	if ctx == nil {
		t.Fatal("baseContext returned nil")
	}
	select {
	case <-ctx.Done():
		t.Fatal("default base context should not be cancelled")
	default:
	}
}

// Once the server shutdown context is wired, cancelling it (graceful shutdown)
// cancels the base context the background review forks derive their timeout
// from, so an in-flight genesis review terminates instead of orphaning.
func TestNudger_ShutdownContextCancels(t *testing.T) {
	n := NewNudger(nil, NudgerConfig{}, nil)
	shutdown, cancel := context.WithCancel(context.Background())
	n.SetShutdownContext(shutdown)

	select {
	case <-n.baseContext().Done():
		t.Fatal("base context done before shutdown")
	default:
	}

	cancel() // simulate graceful server shutdown

	select {
	case <-n.baseContext().Done():
		// expected: the review's base context now reflects the shutdown
	default:
		t.Fatal("base context not cancelled after shutdown ctx cancel")
	}
}

// SetShutdownContext is nil-receiver safe, matching the other Nudger methods so
// callers can install it unconditionally.
func TestNudger_SetShutdownContextNilSafe(t *testing.T) {
	var n *Nudger
	n.SetShutdownContext(context.Background())
}
