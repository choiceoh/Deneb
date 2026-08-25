// session_blackboard.go — session-persistent L₂ state (bounded memory
// contract, slice 2; docs/research/bounded-memory-contract.md).
//
// The blackboard used to be created fresh per RUN: a long multi-turn task
// planned its steps, filled its typed keys — and lost the whole board at the
// next user message, exactly the failure the contract doc names (task state
// has no home but the transcript, and 49% of long runs hit context-pressure,
// #4684). This registry gives the board a session lifetime instead: the same
// board object is handed to every run of a session, and its state re-enters
// each turn as a bounded tail block on the last user message — the one
// sanctioned home for per-turn variable bytes (prompt-cache.md §1.5; system
// bytes must stay frozen, and recall/skill hints already ride this path).
//
// In-memory and best-effort by design, the same discipline as the recall
// citation registry (recall_injected.go): a gateway restart loses board
// state, nothing more. /reset clears the slot — a clean slate must not
// resurrect last week's plan.
package chat

import (
	"os"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

const (
	// sessionBoardTTL evicts boards whose session went quiet — a stale plan
	// re-entering a conversation weeks later is worse than none.
	sessionBoardTTL = 24 * time.Hour
	// sessionBoardMaxSessions bounds the registry; oldest-idle evicted first.
	sessionBoardMaxSessions = 256
	// sessionBoardTailMaxRunes bounds the per-turn tail render. Smaller than
	// the spawn handoff budget: this recurs EVERY turn of the session, and the
	// model can always `blackboard get` for full values.
	sessionBoardTailMaxRunes = 2000
)

type sessionBoardEntry struct {
	board    *toolport.Blackboard
	lastUsed time.Time
}

var sessionBoards = struct {
	mu    sync.Mutex
	store map[string]*sessionBoardEntry
}{store: make(map[string]*sessionBoardEntry)}

// sessionBoardEnabled is the layer kill switch (DENEB_SESSION_BOARD=0), the
// same per-layer attribution discipline as DENEB_SPAWN_L2.
func sessionBoardEnabled() bool {
	return os.Getenv("DENEB_SESSION_BOARD") != "0"
}

// sessionBlackboard returns the session's persistent board, creating it on
// first use. With the layer disabled (or no session key — eval harness paths)
// it returns a fresh run-scoped board, the exact pre-slice behavior.
func sessionBlackboard(sessionKey string, now time.Time) *toolport.Blackboard {
	if sessionKey == "" || !sessionBoardEnabled() {
		return toolport.NewBlackboard()
	}
	sessionBoards.mu.Lock()
	defer sessionBoards.mu.Unlock()
	pruneSessionBoardsLocked(now)
	entry := sessionBoards.store[sessionKey]
	if entry == nil {
		entry = &sessionBoardEntry{board: toolport.NewBlackboard()}
		sessionBoards.store[sessionKey] = entry
	}
	entry.lastUsed = now
	return entry.board
}

// peekSessionBlackboard returns the session's existing board WITHOUT creating
// one — the tail render path must never mint a throwaway board just to render
// it empty (and must not bump lastUsed: rendering is not use).
func peekSessionBlackboard(sessionKey string) *toolport.Blackboard {
	if sessionKey == "" || !sessionBoardEnabled() {
		return nil
	}
	sessionBoards.mu.Lock()
	defer sessionBoards.mu.Unlock()
	if entry := sessionBoards.store[sessionKey]; entry != nil {
		return entry.board
	}
	return nil
}

// clearSessionBlackboard drops the session's board (/reset, clean slate).
func clearSessionBlackboard(sessionKey string) {
	sessionBoards.mu.Lock()
	defer sessionBoards.mu.Unlock()
	delete(sessionBoards.store, sessionKey)
}

// pruneSessionBoardsLocked applies TTL, then the size cap (oldest idle first).
func pruneSessionBoardsLocked(now time.Time) {
	for key, entry := range sessionBoards.store {
		if now.Sub(entry.lastUsed) > sessionBoardTTL {
			delete(sessionBoards.store, key)
		}
	}
	if over := len(sessionBoards.store) - sessionBoardMaxSessions; over > 0 {
		// One sort instead of `over` full scans — the cap is small, but the
		// quadratic shape was noted in review and costs nothing to retire.
		type aged struct {
			key      string
			lastUsed time.Time
		}
		entries := make([]aged, 0, len(sessionBoards.store))
		for key, entry := range sessionBoards.store {
			entries = append(entries, aged{key: key, lastUsed: entry.lastUsed})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].lastUsed.Before(entries[j].lastUsed) })
		for _, e := range entries[:over] {
			delete(sessionBoards.store, e.key)
		}
	}
}

// sessionBoardTail renders the board's state for the turn's tail additions,
// or "" when there is nothing to carry. Rides the same last-user-message slot
// as recall evidence — never the system prompt.
func sessionBoardTail(board *toolport.Blackboard) string {
	if board == nil || !sessionBoardEnabled() {
		return ""
	}
	rendered := board.RenderHandoff(sessionBoardTailMaxRunes)
	if rendered == "" {
		return ""
	}
	return "## 작업 상태 (세션 blackboard — blackboard 툴로 갱신하라)\n" + rendered
}
