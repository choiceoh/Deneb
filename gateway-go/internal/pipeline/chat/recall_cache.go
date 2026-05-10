// recall_cache.go implements P1 of the Hermes-inspired prompt-cache work:
// a lazy, session-frozen snapshot of buildRecallPreflight's output.
//
// Why this exists:
//   - buildRecallPreflight runs wiki/diary/transcript/polaris searches every
//     turn whenever a recall cue is detected. Each search has a 1.5s timeout
//     and the combined latency is real.
//   - Because the result is interpolated into the system prompt's Dynamic
//     block (see prompt/system_prompt.go), per-turn variation also forecloses
//     any future opportunity to cache the dynamic block (the P0 work removed
//     its cache_control marker for exactly this reason — once recall is
//     stable enough to share a marker, it can be re-added).
//
// Lazy semantics:
//   - "First evidence-bearing recall" wins, not "first turn". A cold session
//     where the opening message has no cue contributes nothing to the cache;
//     the next turn that produces real evidence becomes the snapshot.
//   - formatRecallNoEvidence stub responses are not cached so the session can
//     still graduate to a real snapshot once a meaningful cue arrives.
//   - Once the snapshot is set it is frozen for the remainder of the session.
//     A later cue that would have produced different evidence is intentionally
//     ignored — the agent can still pull fresh context via the wiki tool.
//
// Reset:
//   - The /reset slash handler (slash_dispatch.go) clears the snapshot.
//   - Session deletion via other paths (timeout, abort) does NOT currently
//     clear the snapshot; the cache simply lingers until /reset or process
//     restart. This is acceptable for the single-operator deployment but is
//     a candidate for a future PhaseEnd lifecycle hook.
package chat

import (
	"strings"
	"sync"
)

// recallSnapshotStore holds the frozen recall preflight per session.
var recallSnapshotStore = struct {
	mu    sync.RWMutex
	store map[string]string
}{store: make(map[string]string)}

// cachedRecallMemory returns the frozen recall snapshot for sessionKey if one
// has been recorded, plus a hit/miss flag.
func cachedRecallMemory(sessionKey string) (string, bool) {
	if sessionKey == "" {
		return "", false
	}
	recallSnapshotStore.mu.RLock()
	defer recallSnapshotStore.mu.RUnlock()
	v, ok := recallSnapshotStore.store[sessionKey]
	return v, ok
}

// storeRecallMemory records value as the frozen snapshot for sessionKey.
// First-write-wins: if a snapshot already exists, the call is a no-op so
// concurrent turns racing to build recall cannot clobber an earlier store.
// This makes the lazy-frozen invariant atomic — once a session has a
// snapshot it stays that snapshot until clearRecallMemory.
//
// Empty values are ignored so a no-cue / no-evidence turn does not poison
// the cache for the rest of the session.
func storeRecallMemory(sessionKey, value string) {
	if sessionKey == "" || value == "" {
		return
	}
	recallSnapshotStore.mu.Lock()
	defer recallSnapshotStore.mu.Unlock()
	if _, ok := recallSnapshotStore.store[sessionKey]; ok {
		return
	}
	recallSnapshotStore.store[sessionKey] = value
}

// clearRecallMemory drops the snapshot for sessionKey. Called by /reset and
// safe to invoke for sessions that never had a snapshot.
func clearRecallMemory(sessionKey string) {
	if sessionKey == "" {
		return
	}
	recallSnapshotStore.mu.Lock()
	defer recallSnapshotStore.mu.Unlock()
	delete(recallSnapshotStore.store, sessionKey)
}

// recallMemoryHasEvidence reports whether a buildRecallPreflight string carries
// real wiki/diary/transcript/session evidence. The formatRecallNoEvidence stub
// uses a single source line with `source=none`; real evidence rows use
// source=wiki / source=diary / source=transcript / source=session, so the
// absence of `source=none` distinguishes a useful snapshot from a stub.
func recallMemoryHasEvidence(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return !strings.Contains(s, "source=none")
}
