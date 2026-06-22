// notebook_grounding_cache.go memoizes a bound session's notebook grounding
// block so the run loop does not rebuild it (live wiki reads + the byte-budget
// loop) on every turn. Keyed by sessionKey; the entry records the notebook id
// and its Updated stamp, so any pin/unpin/mode change (all bump Updated) or a
// switch to another notebook rebuilds, while an unchanged notebook reuses the
// block. Mirrors tier1_cache.go (cleared by /reset).
//
// Staleness: a wiki *page* edit does not bump the notebook's Updated, so a
// wiki-source's live text is frozen for the session until the notebook is
// mutated or /reset — the same staleness tier1_cache.go accepts. note/file/
// url/mail/diary sources are add-time snapshots, so they are never stale here.
package chat

import "sync"

type notebookGroundingEntry struct {
	notebookID string
	updated    int64
	text       string
}

var notebookGroundingStore = struct {
	mu    sync.RWMutex
	store map[string]notebookGroundingEntry
}{store: make(map[string]notebookGroundingEntry)}

// cachedNotebookGrounding returns the frozen grounding block for the session
// when it matches the current (notebookID, updated); otherwise a miss.
func cachedNotebookGrounding(sessionKey, notebookID string, updated int64) (string, bool) {
	if sessionKey == "" {
		return "", false
	}
	notebookGroundingStore.mu.RLock()
	defer notebookGroundingStore.mu.RUnlock()
	e, ok := notebookGroundingStore.store[sessionKey]
	if ok && e.notebookID == notebookID && e.updated == updated {
		return e.text, true
	}
	return "", false
}

// storeNotebookGrounding records the session's grounding block under its
// content version. Latest-write-wins (the version key handles invalidation).
func storeNotebookGrounding(sessionKey, notebookID string, updated int64, text string) {
	if sessionKey == "" || text == "" {
		return
	}
	notebookGroundingStore.mu.Lock()
	defer notebookGroundingStore.mu.Unlock()
	notebookGroundingStore.store[sessionKey] = notebookGroundingEntry{notebookID, updated, text}
}

// clearNotebookGrounding drops the session's cached grounding. Called by /reset
// alongside clearRecallMemory/clearTier1Wiki; safe for sessions with no entry.
func clearNotebookGrounding(sessionKey string) {
	if sessionKey == "" {
		return
	}
	notebookGroundingStore.mu.Lock()
	defer notebookGroundingStore.mu.Unlock()
	delete(notebookGroundingStore.store, sessionKey)
}
