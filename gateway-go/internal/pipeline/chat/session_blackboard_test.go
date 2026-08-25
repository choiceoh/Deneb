package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// The slice's whole point: the SAME board object survives run boundaries, so
// a plan built in turn 1 is still there in turn 2 — and /reset severs it.
func TestSessionBlackboardPersistsAcrossRunsUntilReset(t *testing.T) {
	now := time.Now()
	key := "client:test:board-persist"
	t.Cleanup(func() { clearSessionBlackboard(key) })

	first := sessionBlackboard(key, now)
	if err := first.Put("goal", json.RawMessage(`"배포"`), "run1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	second := sessionBlackboard(key, now.Add(time.Minute))
	if second != first {
		t.Fatal("second run received a different board — state lost at the run boundary")
	}
	if _, ok := second.Get("goal"); !ok {
		t.Fatal("turn-1 state missing in turn 2")
	}

	clearSessionBlackboard(key)
	third := sessionBlackboard(key, now.Add(2*time.Minute))
	if _, ok := third.Get("goal"); ok {
		t.Fatal("/reset must sever the board, not resurrect it")
	}
}

// Sessionless callers (eval harness) and the kill switch must both get a
// fresh run-scoped board — the exact pre-slice behavior.
func TestSessionBlackboardFallsBackToRunScoped(t *testing.T) {
	now := time.Now()
	if a, b := sessionBlackboard("", now), sessionBlackboard("", now); a == b {
		t.Error("empty session key must never share state")
	}
	t.Setenv("DENEB_SESSION_BOARD", "0")
	key := "client:test:board-killed"
	if a, b := sessionBlackboard(key, now), sessionBlackboard(key, now); a == b {
		t.Error("kill switch must restore run-scoped boards")
	}
}

// A quiet session's stale plan must not re-enter weeks later.
func TestSessionBlackboardTTLEvicts(t *testing.T) {
	now := time.Now()
	key := "client:test:board-ttl"
	t.Cleanup(func() { clearSessionBlackboard(key) })
	first := sessionBlackboard(key, now)
	if err := first.Put("k", json.RawMessage(`"v"`), "run1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	later := sessionBlackboard(key, now.Add(sessionBoardTTL+time.Hour))
	if later == first {
		t.Fatal("board survived past its TTL")
	}
}

// The tail render carries state and the update instruction; an empty board
// contributes nothing to the turn.
func TestSessionBoardTailRendersOnlyWhenStateExists(t *testing.T) {
	board := toolport.NewBlackboard()
	if got := sessionBoardTail(board); got != "" {
		t.Errorf("empty board rendered a tail: %q", got)
	}
	if err := board.Put("step", json.RawMessage(`"검증"`), "run"); err != nil {
		t.Fatalf("put: %v", err)
	}
	got := sessionBoardTail(board)
	if !strings.Contains(got, "세션 blackboard") || !strings.Contains(got, "step") {
		t.Errorf("tail missing header or state: %q", got)
	}
	if got2 := buildTailAdditions(RunParams{}, "", "", "", got); len(got2) != 1 || got2[0] != got {
		t.Errorf("board tail not carried into tail additions: %v", got2)
	}
}

// Ephemeral turns must leave no trace: no persistent board is minted for
// them, and rendering a tail for a session must never create a board either.
func TestSessionBoardLeavesNoTraceForEphemeralAndPeek(t *testing.T) {
	key := "client:test:board-ephemeral"
	t.Cleanup(func() { clearSessionBlackboard(key) })

	if got := peekSessionBlackboard(key); got != nil {
		t.Fatal("peek minted a board for a session that never used one")
	}
	sessionBoards.mu.Lock()
	_, exists := sessionBoards.store[key]
	sessionBoards.mu.Unlock()
	if exists {
		t.Fatal("peek left a registry entry behind")
	}

	// A real session's board IS visible to peek.
	board := sessionBlackboard(key, time.Now())
	if err := board.Put("k", json.RawMessage(`"v"`), "run"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := peekSessionBlackboard(key); got != board {
		t.Fatal("peek must return the session's existing board")
	}
}
