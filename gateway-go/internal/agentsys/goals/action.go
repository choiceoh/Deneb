package goals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
)

// Process-wide default store, shared by the /goal command (chat layer) and the
// goal loop driver (server layer) so both see one source of truth without
// threading the store through every dependency struct. Mirrors the
// pilot.LocalAIHub singleton pattern already used in this codebase.
var (
	defMu sync.RWMutex
	def   *Store
)

// SetDefault installs the process goal store. Called once at startup.
func SetDefault(s *Store) {
	defMu.Lock()
	def = s
	defMu.Unlock()
}

// Default returns the process goal store, or nil when goals are not wired
// (callers must nil-check, exactly like the dormant-by-default integrations).
func Default() *Store {
	defMu.RLock()
	defer defMu.RUnlock()
	return def
}

// mutatingActions are the verbs that make an action-parameterized tool (gmail,
// dropbox, calendar, cron, …) destructive. Read verbs (list/search/read/get…)
// are absent, so they are never ledgered and never blocked.
var mutatingActions = map[string]bool{
	"send": true, "reply": true, "forward": true,
	"upload": true, "share": true, "backup": true,
	"create": true, "update": true, "delete": true,
	"add": true, "remove": true, "write": true, "edit": true,
}

// alwaysDestructive tools cause external/side-effecting work regardless of args
// (the send tool always sends; exec always runs a command).
var alwaysDestructive = map[string]bool{
	"message": true,
	"exec":    true,
}

// DestructiveActionKey classifies a tool call for the idempotency ledger. It
// returns (key, true) when the call is a destructive/external action that must
// not be repeated across goal runs, or ("", false) for read-only calls.
//
// The key is stable across identical calls — args are canonicalized (object
// keys sorted) before hashing — so the same email send produces the same key
// on a re-driven run and the before-tool guard blocks the duplicate.
func DestructiveActionKey(name string, input []byte) (string, bool) {
	destructive := alwaysDestructive[name]
	if !destructive {
		var p struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(input, &p) == nil &&
			mutatingActions[strings.ToLower(strings.TrimSpace(p.Action))] {
			destructive = true
		}
	}
	if !destructive {
		return "", false
	}
	return name + ":" + hashArgs(input), true
}

// hashArgs returns a short stable hash of tool arguments, canonicalizing JSON
// object key order so semantically identical calls collide.
func hashArgs(input []byte) string {
	var v any
	if json.Unmarshal(input, &v) == nil {
		if norm, err := json.Marshal(v); err == nil {
			input = norm
		}
	}
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:8]) // 64-bit prefix — ample for per-goal dedup
}
