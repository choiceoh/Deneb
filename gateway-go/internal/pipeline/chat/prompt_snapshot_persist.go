// prompt_snapshot_persist.go persists the per-session frozen system-prompt
// snapshots across gateway restarts.
//
// Why this exists (the latency problem it solves):
//
//	Three inputs to a session's system prompt are frozen on the session's first
//	turn so mid-session writes don't shift the prompt and break the prefix
//	cache: the tier-1 wiki injection (tier1_cache.go), the workspace context
//	files (prompt.WithSessionSnapshot), and per-topic knowledge
//	(prompt.LoadTopicKnowledge). All three live only in memory.
//
//	The auto-deploy timer SIGUSR1s the gateway ~hourly. Every restart drops
//	those in-memory snapshots, so the next turn re-freezes them from the CURRENT
//	wiki/workspace — and the wiki in particular is rewritten constantly (the
//	"분석 → 위키 갱신" doctrine plus dream cycles). The re-frozen bytes differ,
//	the system-prompt tail changes, and on the vLLM APC path (strict byte-prefix
//	over [system][tool schemas][history]) that invalidates the KV cache for the
//	tool schemas AND the entire conversation history of every restored session —
//	a full per-session re-prefill. The engine itself keeps its KV across the
//	gateway restart, so restoring byte-identical snapshots makes the re-prefill
//	disappear.
//
// What it does:
//
//	Mirrors the three frozen snapshots to a single JSON file under the state dir
//	(DENEB_STATE_DIR-aware, so a dev gateway never pollutes the production file).
//	On startup — at the same point session restore repopulates the session
//	manager — the file is read back and pushed into the in-memory stores, so the
//	first post-restart turn of each session sees the SAME frozen bytes it had
//	before the restart. /reset forgets a session's entry; sessions whose
//	transcript is gone (deleted or expired) are pruned at load to bound growth.
//
// Cache doctrine (.claude/rules/prompt-cache.md): the persisted bytes must be
// restored EXACTLY. A JSON round-trip of the snapshot strings is byte-exact, so
// the reconstructed system prompt is identical and the APC prefix survives.
// Restoring is first-write-wins against the live stores so a turn that races
// startup is never clobbered.
package chat

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// promptSnapshotFileName is the on-disk file under the state dir. It sits beside
// autonomous_state.json and goals.json — the other restart-survival state.
const promptSnapshotFileName = "prompt_snapshots.json"

// nativeMainSessionKey is the native client's home session. Only it and its
// explicit sub-conversations (client:main:<id>) survive a restart — see the
// server's isRestorableNativeSessionKey. Persisting anything else (cron/system
// keys) would only bloat the file with entries that are pruned at the next load,
// so record() gates on this set. Keep in sync with that server predicate.
const nativeMainSessionKey = "client:main"

// isRestorablePromptSnapshotSession reports whether a session is one the startup
// restore would wake, hence worth persisting a snapshot for.
func isRestorablePromptSnapshotSession(sessionKey string) bool {
	return sessionKey == nativeMainSessionKey ||
		strings.HasPrefix(sessionKey, nativeMainSessionKey+":")
}

// persistedPromptSnapshot is the on-disk record for one session: every field is
// a frozen system-prompt input. omitempty keeps the file lean for sessions that
// have only some of the three (e.g. no topic mapped).
type persistedPromptSnapshot struct {
	Tier1Wiki      string                 `json:"tier1Wiki,omitempty"`
	ContextFiles   []prompt.ContextFile   `json:"contextFiles,omitempty"`
	TopicKnowledge *prompt.TopicKnowledge `json:"topicKnowledge,omitempty"`
}

// promptSnapshotPersister mirrors the frozen snapshots to disk. The mirror map
// is the serialization source; the live tier1/prompt.Cache stores remain the
// read path during a session. They stay consistent because record() writes the
// mirror with the same first-write-wins gate the live stores use, and load()
// populates both.
type promptSnapshotPersister struct {
	mu     sync.Mutex
	dir    string // state dir; "" disables persistence entirely (no-op)
	logger *slog.Logger
	store  map[string]persistedPromptSnapshot // sessionKey → frozen inputs
}

// promptSnapshots is the process-wide singleton. The data it manages
// (tier1SnapshotStore, prompt.Cache) is itself process-global, so a singleton
// is the natural fit — mirroring how those stores are declared.
var promptSnapshots = &promptSnapshotPersister{logger: slog.Default()}

// ConfigurePromptSnapshots enables snapshot persistence rooted at dir. The
// gateway injects the resolved state dir here (chat must not import config);
// call once during startup before any turn runs. An empty dir leaves
// persistence disabled (in-memory only), matching autonomous's SetStateDir.
func ConfigurePromptSnapshots(dir string, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	promptSnapshots.mu.Lock()
	promptSnapshots.dir = dir
	promptSnapshots.logger = logger.With("subsys", "prompt-snapshot")
	promptSnapshots.mu.Unlock()
}

// LoadPromptSnapshots reads the persisted snapshots and pushes them into the
// live stores, so the first post-restart turn of each session reuses its frozen
// bytes instead of re-freezing from the (now-changed) wiki/workspace. isLive
// reports whether a sessionKey still exists after session restore; entries for
// vanished sessions (deleted/expired) are pruned to bound the file. Returns the
// number of sessions restored. Call after restoreAndWakeSessions.
func LoadPromptSnapshots(isLive func(sessionKey string) bool) int {
	return promptSnapshots.load(isLive)
}

// recordPromptSnapshot folds a turn's frozen snapshot inputs into the mirror
// (first-write-wins per field) and rewrites the file when anything new appears.
// Cheap and idempotent on the common path: once a session's fields are present,
// every later turn is a no-op with no disk I/O.
func recordPromptSnapshot(sessionKey, tier1 string, ctxFiles []prompt.ContextFile, topic *prompt.TopicKnowledge) {
	promptSnapshots.record(sessionKey, tier1, ctxFiles, topic)
}

// forgetPromptSnapshot drops a session's persisted snapshot. Called from
// /reset, the same place the in-memory snapshots are cleared.
func forgetPromptSnapshot(sessionKey string) {
	promptSnapshots.forget(sessionKey)
}

// filePathLocked returns the snapshot file path; caller must hold p.mu.
func (p *promptSnapshotPersister) filePathLocked() string {
	if p.dir == "" {
		return ""
	}
	return filepath.Join(p.dir, promptSnapshotFileName)
}

// cloneLocked returns a shallow copy of the mirror for writing without holding
// the lock during disk I/O. Shallow is safe: the values are immutable once
// frozen (first-write-wins), and the writer only reads them to marshal.
func (p *promptSnapshotPersister) cloneLocked() map[string]persistedPromptSnapshot {
	out := make(map[string]persistedPromptSnapshot, len(p.store))
	for k, v := range p.store {
		out[k] = v
	}
	return out
}

func (p *promptSnapshotPersister) record(sessionKey, tier1 string, ctxFiles []prompt.ContextFile, topic *prompt.TopicKnowledge) {
	if sessionKey == "" || !isRestorablePromptSnapshotSession(sessionKey) {
		return
	}
	p.mu.Lock()
	if p.dir == "" {
		p.mu.Unlock()
		return
	}
	cur := p.store[sessionKey]
	changed := false
	if cur.Tier1Wiki == "" && tier1 != "" {
		cur.Tier1Wiki = tier1
		changed = true
	}
	if len(cur.ContextFiles) == 0 && len(ctxFiles) > 0 {
		cur.ContextFiles = ctxFiles
		changed = true
	}
	if cur.TopicKnowledge == nil && topic != nil && topic.Content != "" {
		cp := *topic
		cur.TopicKnowledge = &cp
		changed = true
	}
	if !changed {
		p.mu.Unlock()
		return
	}
	if p.store == nil {
		p.store = make(map[string]persistedPromptSnapshot)
	}
	p.store[sessionKey] = cur
	path := p.filePathLocked()
	logger := p.logger
	snapshot := p.cloneLocked()
	p.mu.Unlock()

	writePromptSnapshotFile(path, snapshot, logger)
}

func (p *promptSnapshotPersister) forget(sessionKey string) {
	if sessionKey == "" {
		return
	}
	p.mu.Lock()
	if p.dir == "" {
		p.mu.Unlock()
		return
	}
	if _, ok := p.store[sessionKey]; !ok {
		p.mu.Unlock()
		return
	}
	delete(p.store, sessionKey)
	path := p.filePathLocked()
	logger := p.logger
	snapshot := p.cloneLocked()
	p.mu.Unlock()

	writePromptSnapshotFile(path, snapshot, logger)
}

func (p *promptSnapshotPersister) load(isLive func(string) bool) int {
	p.mu.Lock()
	path := p.filePathLocked()
	logger := p.logger
	p.mu.Unlock()
	if path == "" {
		return 0
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("prompt snapshot: read failed", "error", err)
		}
		return 0
	}
	var persisted map[string]persistedPromptSnapshot
	if err := json.Unmarshal(data, &persisted); err != nil {
		// A torn/corrupt file is not fatal — it just means a one-time re-prefill
		// for the affected sessions. Ignore it rather than wrong bytes.
		logger.Warn("prompt snapshot: parse failed; ignoring file", "error", err)
		return 0
	}

	p.mu.Lock()
	if p.store == nil {
		p.store = make(map[string]persistedPromptSnapshot, len(persisted))
	}
	type pending struct {
		key  string
		snap persistedPromptSnapshot
	}
	var toRestore []pending
	pruned := 0
	for key, snap := range persisted {
		if _, exists := p.store[key]; exists {
			// A turn that arrived between server-ready and this load already
			// froze (and re-persisted) this session — first-write-wins, keep it.
			continue
		}
		if isLive != nil && !isLive(key) {
			pruned++ // session deleted/expired: drop it to bound the file
			continue
		}
		p.store[key] = snap
		toRestore = append(toRestore, pending{key, snap})
	}
	var rewrite map[string]persistedPromptSnapshot
	if pruned > 0 {
		rewrite = p.cloneLocked()
	}
	p.mu.Unlock()

	// Push into the live stores outside the lock (concurrency rule: no external
	// store calls under our mutex). First-write-wins so a racing turn's fresh
	// freeze is never overwritten by a stale on-disk value.
	for _, pr := range toRestore {
		restoreSnapshotIntoStores(pr.key, pr.snap)
	}
	if rewrite != nil {
		writePromptSnapshotFile(path, rewrite, logger)
		logger.Info("prompt snapshot: pruned vanished sessions", "pruned", pruned)
	}
	return len(toRestore)
}

// restoreSnapshotIntoStores pushes one persisted snapshot into the live stores,
// first-write-wins against each so a turn that already froze a value this boot
// wins over the on-disk copy.
func restoreSnapshotIntoStores(key string, snap persistedPromptSnapshot) {
	if snap.Tier1Wiki != "" {
		storeTier1Wiki(key, snap.Tier1Wiki) // storeTier1Wiki is itself first-write-wins
	}
	if len(snap.ContextFiles) > 0 {
		if _, ok := prompt.Cache.SessionSnapshot(key); !ok {
			prompt.Cache.SetSessionSnapshot(key, snap.ContextFiles)
		}
	}
	if snap.TopicKnowledge != nil && snap.TopicKnowledge.Content != "" {
		if _, ok := prompt.Cache.TopicSnapshot(key); !ok {
			prompt.Cache.SetTopicSnapshot(key, *snap.TopicKnowledge)
		}
	}
}

// writePromptSnapshotFile atomically writes the mirror to disk (tmp + rename) so
// a crash mid-write cannot leave a half-written file the next boot would parse.
// Best-effort: a failure is logged and never interrupts the turn.
func writePromptSnapshotFile(path string, store map[string]persistedPromptSnapshot, logger *slog.Logger) {
	if path == "" {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	data, err := json.Marshal(store)
	if err != nil {
		logger.Warn("prompt snapshot: marshal failed", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Warn("prompt snapshot: write failed", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("prompt snapshot: rename failed", "error", err)
		_ = os.Remove(tmp)
	}
}
