// tail_register.go — cross-run byte stability for the per-turn tail
// additions (run_tail_inject.go).
//
// The tail additions (recall evidence, feed digest, skill hints, delivery
// directive) are wire-only: the transcript stores the clean user message.
// Within a run that is byte-stable, but at the NEXT run the history reload
// returns the clean message — the request byte-stream diverges at that
// message, which for content-prefix provider caches (kimi: exact-match on
// (system, tools) + strict prefix over messages) discards the ENTIRE
// conversation cache. Live capture 2026-07-20: every run boundary re-billed
// the whole conversation, cache reads pinned at system+tools size.
//
// The register keeps the transcript clean (display, search and export paths
// never see recall blocks) and instead re-attaches each historical user
// message's tail at assembly time, keyed by a hash of the message's clean
// content bytes. Attachment reuses appendTextToMessage with the recorded
// joined-suffix, so the re-attached bytes are identical to what the original
// run sent — the next run's request prefix matches the previous run's cache.
//
// Lock notes: one mutex; disk writes happen on a snapshot outside the lock
// (same pattern as promptSnapshotPersister). Only restorable sessions
// (client:main*) are persisted to disk; other sessions get in-memory
// stability for the process lifetime and one cold re-prefill after restart.
package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// tailRegisterFileName sits beside prompt_snapshots.json in the state dir.
const tailRegisterFileName = "user_message_tails.json"

// tailRegisterMaxPerSession bounds per-session entries; the oldest entries
// fall off first. 200 covers far more history than the context window keeps.
const tailRegisterMaxPerSession = 200

// persistedTailEntry is one recorded tail: the content-hash hex of the clean
// message content bytes and the joined additions suffix (without the leading
// separator — appendTextToMessage adds it identically on every attach).
type persistedTailEntry struct {
	Hash   string `json:"h"`
	Suffix string `json:"suffix"`
}

// tailRegister owns the per-session tail entries. Entries preserve insert
// order for FIFO eviction; index maps hash → entry position.
type tailRegister struct {
	mu       sync.Mutex
	dir      string // "" disables disk persistence (in-memory only)
	logger   *slog.Logger
	sessions map[string][]persistedTailEntry
}

var messageTails = &tailRegister{logger: slog.Default()}

// ConfigureTailRegister enables tail persistence rooted at dir and loads the
// persisted entries. Call once during startup before any turn runs; an empty
// dir keeps the register in-memory only.
func ConfigureTailRegister(dir string, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("subsys", "tail-register")
	messageTails.mu.Lock()
	messageTails.dir = dir
	messageTails.logger = logger
	messageTails.mu.Unlock()
	messageTails.load()
}

// attachPersistedTails returns messages with each user message's recorded
// tail re-attached (copy-on-write; the input slice and its elements are never
// mutated). Messages without a recorded tail pass through byte-identical.
func attachPersistedTails(sessionKey string, messages []llm.Message) []llm.Message {
	if sessionKey == "" || len(messages) == 0 {
		return messages
	}
	messageTails.mu.Lock()
	entries := messageTails.sessions[sessionKey]
	messageTails.mu.Unlock()
	if len(entries) == 0 {
		return messages
	}
	byHash := make(map[string]string, len(entries))
	for _, e := range entries {
		byHash[e.Hash] = e.Suffix
	}
	var out []llm.Message
	for i := range messages {
		if messages[i].Role != "user" {
			continue
		}
		suffix, ok := byHash[tailContentHash(messages[i].Content.Bytes())]
		if !ok {
			continue
		}
		appended, ok := appendTextToMessage(messages[i], []string{suffix})
		if !ok {
			continue
		}
		if out == nil {
			out = make([]llm.Message, len(messages))
			copy(out, messages)
		}
		out[i] = appended
	}
	if out == nil {
		return messages
	}
	return out
}

// recordPersistedTail records the joined tail suffix for a user message's
// clean content bytes so later runs re-attach it. No-op for empty inputs; a
// re-record of the same hash overwrites (last write wins — the wire form of
// the latest run is the one whose cache lineage continues).
func recordPersistedTail(sessionKey string, cleanContent []byte, additions []string) {
	if sessionKey == "" || len(cleanContent) == 0 || len(additions) == 0 {
		return
	}
	entry := persistedTailEntry{
		Hash:   tailContentHash(cleanContent),
		Suffix: strings.Join(additions, "\n\n"),
	}
	messageTails.mu.Lock()
	if messageTails.sessions == nil {
		messageTails.sessions = make(map[string][]persistedTailEntry)
	}
	entries := messageTails.sessions[sessionKey]
	replaced := false
	for i := range entries {
		if entries[i].Hash == entry.Hash {
			if entries[i].Suffix == entry.Suffix {
				messageTails.mu.Unlock()
				return // idempotent re-record: no disk churn
			}
			entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
		if len(entries) > tailRegisterMaxPerSession {
			entries = entries[len(entries)-tailRegisterMaxPerSession:]
		}
	}
	messageTails.sessions[sessionKey] = entries
	path, logger, snapshot := messageTails.persistSnapshotLocked(sessionKey)
	messageTails.mu.Unlock()

	writeTailRegisterFile(path, snapshot, logger)
}

// clearPersistedTails drops a session's tails (called from /reset alongside
// the other frozen-state clears).
func clearPersistedTails(sessionKey string) {
	messageTails.mu.Lock()
	if _, ok := messageTails.sessions[sessionKey]; !ok {
		messageTails.mu.Unlock()
		return
	}
	delete(messageTails.sessions, sessionKey)
	path, logger, snapshot := messageTails.persistSnapshotLocked(sessionKey)
	messageTails.mu.Unlock()

	writeTailRegisterFile(path, snapshot, logger)
}

// persistSnapshotLocked returns the write inputs for a change to sessionKey:
// a nil snapshot (path "") when the change does not persist to disk — either
// persistence is disabled or the session is not a restorable one. Caller must
// hold the mutex.
func (t *tailRegister) persistSnapshotLocked(sessionKey string) (string, *slog.Logger, map[string][]persistedTailEntry) {
	if t.dir == "" || !isRestorablePromptSnapshotSession(sessionKey) {
		return "", t.logger, nil
	}
	snapshot := make(map[string][]persistedTailEntry)
	for key, entries := range t.sessions {
		if isRestorablePromptSnapshotSession(key) {
			snapshot[key] = entries
		}
	}
	return filepath.Join(t.dir, tailRegisterFileName), t.logger, snapshot
}

func (t *tailRegister) load() {
	t.mu.Lock()
	dir := t.dir
	logger := t.logger
	t.mu.Unlock()
	if dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, tailRegisterFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("tail register: read failed", "error", err)
		}
		return
	}
	var persisted map[string][]persistedTailEntry
	if err := json.Unmarshal(data, &persisted); err != nil {
		// Torn/corrupt file → one cold re-prefill per session; never wrong bytes.
		logger.Warn("tail register: parse failed; ignoring file", "error", err)
		return
	}
	t.mu.Lock()
	if t.sessions == nil {
		t.sessions = make(map[string][]persistedTailEntry, len(persisted))
	}
	restored := 0
	for key, entries := range persisted {
		if !isRestorablePromptSnapshotSession(key) {
			continue
		}
		if _, exists := t.sessions[key]; exists {
			continue // a turn already recorded since startup — keep the fresher state
		}
		t.sessions[key] = entries
		restored++
	}
	t.mu.Unlock()
	if restored > 0 {
		logger.Info("tail register: restored persisted tails", "sessions", restored)
	}
}

func writeTailRegisterFile(path string, snapshot map[string][]persistedTailEntry, logger *slog.Logger) {
	if path == "" || snapshot == nil {
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		logger.Warn("tail register: marshal failed", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Warn("tail register: write failed", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("tail register: rename failed", "error", err)
	}
}

// tailContentHash keys a message by its clean content bytes.
func tailContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
