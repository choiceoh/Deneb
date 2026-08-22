package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// /reset must reclaim the session's spill files.
//
// The lifecycle hook cannot cover this: /reset finishes by emitting PhaseEnd,
// the same terminal status an ordinary completed run emits, and runs must not
// release spills (their read_spillover pointers stay quoted in surviving
// history). Reset is the opposite case — the history that quoted them is gone —
// so the reclaim is explicit in handleResetCommand. Without it the TTL sweep
// also holds them, because the session row survives a reset.
func TestResetCommandReclaimsSpillover(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true }) // session survives /reset

	const sessionKey = "client:main"
	spillID, err := store.Store(sessionKey, "exec", strings.Repeat("x", 1024))
	if err != nil {
		t.Fatalf("seed spill: %v", err)
	}
	otherID, err := store.Store("client:other", "exec", strings.Repeat("y", 1024))
	if err != nil {
		t.Fatalf("seed sibling spill: %v", err)
	}

	reg := NewToolRegistry()
	reg.SetSpilloverStore(store)
	h := &Handler{
		logger:      discardLogger(),
		tools:       reg,
		sessions:    session.NewManager(),
		abort:       NewAbortTracker(),
		pending:     NewPendingQueue(),
		mergeWindow: NewMergeWindowTracker(),
	}

	h.handleResetCommand(sessionKey, func(string) {})

	if _, err := store.Load(spillID, sessionKey); err == nil {
		t.Error("/reset left the session's spill files behind — disk is never reclaimed")
	}
	if _, err := store.Load(otherID, "client:other"); err != nil {
		t.Errorf("/reset reclaimed a sibling session's spill: %v", err)
	}
}
