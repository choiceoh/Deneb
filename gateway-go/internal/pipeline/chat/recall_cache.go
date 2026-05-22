// recall_cache.go implements the recall snapshot cache.
//
// Why this exists:
//   - buildRecallPreflight runs wiki/diary/transcript/polaris searches every
//     turn whenever a recall cue is detected. Each search has a 1.5s timeout
//     and the combined latency is real.
//   - Repeated user questions about the same topic should reuse that work
//     instead of re-running every dependency.
//
// Cache key: (sessionKey, cueFingerprint).
//
//   - cueFingerprint is derived from the recall cue + sorted signal terms of
//     the user message (see recallCueFingerprint). Two turns asking about the
//     same topic share a slot; two turns asking about different topics get
//     independent slots. This kills the "first turn's recall freezes the
//     entire session" failure mode where a turn-1 recall about topic A would
//     remain injected into the system prompt for every subsequent turn,
//     including turns about unrelated topics.
//
//   - An empty fingerprint means the current turn has no recall cue, so the
//     per-fingerprint cache is bypassed entirely. Hindsight auto-recall may
//     still run for such turns, but uncached: every no-cue turn issues a
//     fresh memory-bank query rather than reusing a stale snapshot.
//
// First-write-wins is preserved per (session, fingerprint) so concurrent
// turns racing to fill the same slot cannot clobber an earlier store.
//
// Reset:
//   - The /reset slash handler clears every entry for the session.
//   - Session deletion via other paths (timeout, abort) does NOT currently
//     clear entries; cache simply lingers until /reset or process restart.
//     Acceptable for the single-operator deployment.
package chat

import (
	"sort"
	"strings"
	"sync"
)

// recallSnapshotKey identifies one recall cache entry.
type recallSnapshotKey struct {
	SessionKey  string
	Fingerprint string
}

var recallSnapshotStore = struct {
	mu    sync.RWMutex
	store map[recallSnapshotKey]string
}{store: make(map[recallSnapshotKey]string)}

// cachedRecallMemory returns the frozen recall snapshot for (sessionKey,
// fingerprint) if one has been recorded, plus a hit/miss flag.
func cachedRecallMemory(sessionKey, fingerprint string) (string, bool) {
	if sessionKey == "" {
		return "", false
	}
	recallSnapshotStore.mu.RLock()
	defer recallSnapshotStore.mu.RUnlock()
	v, ok := recallSnapshotStore.store[recallSnapshotKey{sessionKey, fingerprint}]
	return v, ok
}

// storeRecallMemory records value as the snapshot for (sessionKey,
// fingerprint). First-write-wins per slot. Empty session or value is a no-op.
func storeRecallMemory(sessionKey, fingerprint, value string) {
	if sessionKey == "" || value == "" {
		return
	}
	recallSnapshotStore.mu.Lock()
	defer recallSnapshotStore.mu.Unlock()
	key := recallSnapshotKey{sessionKey, fingerprint}
	if _, ok := recallSnapshotStore.store[key]; ok {
		return
	}
	recallSnapshotStore.store[key] = value
}

// clearRecallMemory drops every snapshot belonging to sessionKey across all
// fingerprints. Called by /reset and safe to invoke for sessions that never
// had a snapshot.
func clearRecallMemory(sessionKey string) {
	if sessionKey == "" {
		return
	}
	recallSnapshotStore.mu.Lock()
	defer recallSnapshotStore.mu.Unlock()
	for k := range recallSnapshotStore.store {
		if k.SessionKey == sessionKey {
			delete(recallSnapshotStore.store, k)
		}
	}
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

// recallCueFingerprint returns a stable identifier for the user's current
// recall intent based on the message's cue + signal terms.
//
//   - Empty string when the message has no recall cue — caller skips the
//     cue-gated cache for that turn (Hindsight auto-recall may still run).
//   - "cue-only" when there is a cue phrase but no specific signal terms
//     (e.g., "그거 뭐였지?"). All such turns share one slot because the
//     recall search falls back to the same recent-diary entries.
//   - Otherwise the sorted, pipe-joined signal terms.
//
// Two messages on the same topic produce the same fingerprint and share a
// cache slot; different topics get different fingerprints.
func recallCueFingerprint(message string) string {
	if !shouldRunRecallPreflight(message) {
		return ""
	}
	terms := recallSignalTerms(message)
	if len(terms) == 0 {
		return "cue-only"
	}
	sorted := append([]string(nil), terms...)
	sort.Strings(sorted)
	return strings.Join(sorted, "|")
}
