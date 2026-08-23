// tier1_cache.go freezes the tier-1 wiki injection ("핵심 지식") per session.
//
// Why: knowledge.FormatTier1 reads the LIVE wiki store on every prep, and the
// agent writes wiki pages mid-session constantly (the "분석 → 위키 갱신"
// doctrine, plus dream cycles). Each write shifted the system-prompt tail,
// which on the vLLM APC path invalidated the KV cache of the tool schemas
// and the entire conversation history (strict prefix matching over
// [system][tools][history]). Freezing the snapshot on first use keeps the
// system prompt byte-stable for the session — the same semantics the context
// files already have via prompt.WithSessionSnapshot.
//
// Staleness trade-off: knowledge written mid-session appears from the next
// session (or /reset) onward, matching the context-file and topic-knowledge
// snapshots. The live `wiki` tool remains the authoritative read path.
//
// Mirrors recall_cache.go: first-write-wins, non-empty values only (an empty
// first result — e.g. a store still warming at boot — retries next turn; a
// store that is genuinely empty recomputes to "" every turn, which is
// byte-stable anyway), /reset clears.
package chat

import "sync"

var tier1SnapshotStore = struct {
	mu         sync.RWMutex
	store      map[string]string // sessionKey → frozen tier-1 injection
	generation uint64
}{store: make(map[string]string)}

// cachedTier1Wiki returns the frozen tier-1 snapshot for the session.
func cachedTier1Wiki(sessionKey string) (string, bool) {
	value, ok, _ := cachedTier1WikiWithGeneration(sessionKey)
	return value, ok
}

func cachedTier1WikiWithGeneration(sessionKey string) (string, bool, uint64) {
	if sessionKey == "" {
		return "", false, 0
	}
	tier1SnapshotStore.mu.RLock()
	defer tier1SnapshotStore.mu.RUnlock()
	v, ok := tier1SnapshotStore.store[sessionKey]
	return v, ok, tier1SnapshotStore.generation
}

// storeTier1Wiki records the session's tier-1 snapshot. First-write-wins;
// empty session or value is a no-op.
func storeTier1Wiki(sessionKey, value string) {
	if sessionKey == "" || value == "" {
		return
	}
	tier1SnapshotStore.mu.Lock()
	defer tier1SnapshotStore.mu.Unlock()
	if _, ok := tier1SnapshotStore.store[sessionKey]; ok {
		return
	}
	tier1SnapshotStore.store[sessionKey] = value
}

func storeTier1WikiIfGeneration(sessionKey, value string, generation uint64) bool {
	if sessionKey == "" || value == "" {
		return false
	}
	tier1SnapshotStore.mu.Lock()
	defer tier1SnapshotStore.mu.Unlock()
	if generation != tier1SnapshotStore.generation {
		return false
	}
	if _, ok := tier1SnapshotStore.store[sessionKey]; ok {
		return false
	}
	tier1SnapshotStore.store[sessionKey] = value
	return true
}

// clearTier1Wiki drops the session's snapshot. Called by /reset alongside
// recall.ClearSession; safe for sessions that never stored one.
func clearTier1Wiki(sessionKey string) {
	if sessionKey == "" {
		return
	}
	tier1SnapshotStore.mu.Lock()
	defer tier1SnapshotStore.mu.Unlock()
	delete(tier1SnapshotStore.store, sessionKey)
}

func clearAllTier1Wiki() {
	tier1SnapshotStore.mu.Lock()
	defer tier1SnapshotStore.mu.Unlock()
	tier1SnapshotStore.generation++
	tier1SnapshotStore.store = make(map[string]string)
}
