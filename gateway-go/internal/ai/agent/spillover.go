// Package agent — SpilloverStore saves large tool results to disk and returns
// compact previews for the LLM context window.
//
// The actual spill threshold is DefaultMaxOutput (see truncate.go): any tool
// result longer than that is written to
// ~/.deneb/spillover/{session}_{ts}_{tool}_{hash}.txt by ToolRegistry.Execute
// and the in-context text is replaced with a head+tail preview embedding the
// spill ID. The LLM then retrieves the full content on demand via the
// read_spillover tool. MaxResultChars below is the larger "hard cap" used in
// tests to size fixtures and document the upper bound; it does not trigger
// spills directly.
//
// Lifetime: a spill lives as long as its session. Compaction clears older tool
// results but keeps their read_spillover pointer, telling the model the full
// output is "still available" (pipeline/compaction/restore.go) — so expiring a
// spill while its session is still live turns that promise into a dangling
// handle. The session-end hook (runtime/server/server_spillover_lifecycle.go)
// is therefore the primary reclaim path, and the TTL sweep below is demoted to
// an orphan collector: it only removes spills whose session is already gone.
package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Spillover thresholds.
const (
	MaxResultChars   = 32 * 1024 // 32K chars — results larger than this are spilled
	PreviewHeadChars = 1024      // first 1K chars in preview
	PreviewTailChars = 1024      // last 1K chars in preview
	SpilloverTTL     = 30 * time.Minute
	cleanupInterval  = 10 * time.Minute

	// hashInputLimit caps content bytes fed into the spillover ID hash.
	hashInputLimit = 256
	// hashIDBytes is the number of SHA-256 bytes used for the spill ID (8 hex chars).
	hashIDBytes = 4
	// sessionHashBytes is the number of SHA-256 bytes of the session key baked
	// into a spill filename (8 hex chars) so nested keys stay distinguishable.
	sessionHashBytes = 4

	// Per-session retention bounds. A session's spills live as long as the
	// session, and conversation rows are deliberately never evicted
	// (domain/session/manager.go keeps them as the drawer's history), so
	// "session is alive" is an unlimited lease for the busiest sessions — the
	// TTL sweep skips them forever. These caps put a ceiling on that: a long
	// conversation doing repeated large exec/read work cannot grow the spill
	// directory without bound.
	//
	// Eviction is oldest-first, so the handles compaction most recently quoted
	// survive. An evicted handle reports not-found and the model re-runs the
	// tool — the same contract as a spill from an ended session.
	maxSpillsPerSession = 48
)

// maxSpillBytesPerSession is a var, not a const, only so tests can shrink it —
// the clamp below is otherwise unobservable without allocating half a gigabyte.
// Nothing at runtime writes it.
var maxSpillBytesPerSession = 512 * 1024 * 1024

// spillClampNotice terminates a spill that was itself larger than the whole
// session ceiling, so the model reading it knows the tail is gone rather than
// concluding the tool produced nothing more.
const spillClampNotice = "\n\n…[이 결과는 세션 스필 상한을 넘어 뒷부분이 잘렸습니다 — 도구를 더 좁은 범위로 다시 실행하세요]\n"

// spillEntry tracks a single spilled result on disk.
type spillEntry struct {
	Path       string
	SessionKey string
	ToolName   string
	OrigLen    int
	CreatedAt  time.Time
	// ExternalOrigin marks content that came from outside the operator's trust
	// boundary. It is a separate bit rather than a lookup on ToolName because
	// the producing tool is not always the sourcing tool: code_action reads mail
	// or web pages through its bridge and spills them under its own name, so the
	// name alone would call attacker-authored text operator-owned.
	ExternalOrigin bool
}

// SpilloverStore manages disk-backed large tool results.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	SpilloverStore.mu
//	SpilloverStore.livenessMu (independent — never held together with mu)
//
// sessionAlive is an injected callback into the session manager, so it is read
// under livenessMu and invoked with mu released (concurrency.md §3).
type SpilloverStore struct {
	baseDir string
	mu      sync.RWMutex
	index   map[string]*spillEntry // spill_id → metadata

	livenessMu   sync.RWMutex
	sessionAlive func(sessionKey string) bool
}

// NewSpilloverStore creates a store rooted at baseDir (e.g. ~/.deneb/spillover).
// The directory is created lazily on the first Store call.
func NewSpilloverStore(baseDir string) *SpilloverStore {
	return &SpilloverStore{
		baseDir: baseDir,
		index:   make(map[string]*spillEntry),
	}
}

// SetSessionLiveness injects the predicate the TTL sweep uses to tell a live
// session from a finished one. Without it the sweep falls back to pure age,
// which is the pre-existing behaviour (and expires spills that compaction
// stubs still point at). Wired once at composition time, before StartCleanup.
func (s *SpilloverStore) SetSessionLiveness(alive func(sessionKey string) bool) {
	s.livenessMu.Lock()
	s.sessionAlive = alive
	s.livenessMu.Unlock()
}

// isSessionAlive reports whether sessionKey still has a live session. It must
// be called with s.mu released: the predicate reaches into the session manager
// and taking an unrelated lock under s.mu would invert the hierarchy.
func (s *SpilloverStore) isSessionAlive(sessionKey string) bool {
	s.livenessMu.RLock()
	alive := s.sessionAlive
	s.livenessMu.RUnlock()
	if alive == nil {
		return false
	}
	return alive(sessionKey)
}

// Store writes content to disk and returns the spill ID.
//
// Content is passed through pkg/redact before persistence so large tool
// outputs (e.g. `cat .env`, curl responses) never put raw secrets on disk.
// The spill ID is hashed over the original content so retrieval works even if
// a subsequent call sees different redaction results; only the file bytes are
// masked. When redaction is disabled the content is stored verbatim.
func (s *SpilloverStore) Store(sessionKey, toolName, content string) (string, error) {
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return "", fmt.Errorf("spillover mkdir: %w", err)
	}

	now := time.Now()
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", content[:min(hashInputLimit, len(content))], now.UnixNano(), sessionKey)))
	spillID := fmt.Sprintf("sp_%x", hash[:hashIDBytes])

	safeTool := sanitizeToolName(toolName)
	filename := fmt.Sprintf("%s_%d_%s_%s.txt", sessionFilePrefix(sessionKey), now.UnixMilli(), safeTool, spillID)
	path := filepath.Join(s.baseDir, filename)

	persisted := redact.String(content)
	// A single result bigger than the whole session ceiling would otherwise sit
	// on disk exempt from it: enforceSessionQuotaLocked spares the spill it is
	// handing a handle for, so nothing can evict it and the advertised ceiling
	// is exceeded by an arbitrary amount for as long as the session lives.
	//
	// Clamp the file rather than refusing the spill. Refusing leaves
	// capToolOutput with no handle to put in the truncation marker, which makes
	// the discarded middle unrecoverable — a worse failure than losing the tail
	// of a result that was already past anything a session can hold.
	if len(persisted) > maxSpillBytesPerSession {
		persisted = textutil.TruncateBytes(persisted, maxSpillBytesPerSession-len(spillClampNotice)) + spillClampNotice
	}
	if err := os.WriteFile(path, []byte(persisted), 0o644); err != nil { //nolint:gosec // G306 — world-readable is intentional
		return "", fmt.Errorf("spillover write: %w", err)
	}

	s.mu.Lock()
	s.index[spillID] = &spillEntry{
		Path:       path,
		SessionKey: sessionKey,
		ToolName:   toolName,
		OrigLen:    len(persisted),
		CreatedAt:  now,
		// Classify here, not at the call sites: every writer (tool registry,
		// the YouTube transcript path, anything added later) gets the safe
		// default without having to remember a follow-up call.
		ExternalOrigin: IsExternalOriginTool(toolName),
	}
	evicted := s.enforceSessionQuotaLocked(sessionKey, spillID)
	s.mu.Unlock()

	for _, p := range evicted {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("spillover quota evict failed", "path", p, "err", err)
		}
	}

	return spillID, nil
}

// Load reads the full content of a spilled result.
// Returns an error if the ID is unknown or belongs to a different session.
// The unknown-ID error carries the session's live spill IDs: the observed
// failure mode is the model passing a made-up name ("morning_letter") instead
// of the sp_* ID from the preview marker, and a bare "not found" gives it
// nothing to self-correct with on the next turn.
func (s *SpilloverStore) Load(spillID, sessionKey string) (string, error) {
	s.mu.RLock()
	entry, ok := s.index[spillID]
	var available []string
	if !ok {
		for id, e := range s.index {
			if e.SessionKey == sessionKey {
				available = append(available, id)
			}
		}
	}
	s.mu.RUnlock()

	if !ok {
		if len(available) > 0 {
			sort.Strings(available)
			return "", fmt.Errorf("spillover ID %q not found — use the exact sp_* ID from the [SpillOver: ID=…] marker; this session's live spill IDs: %s",
				spillID, strings.Join(available, ", "))
		}
		return "", fmt.Errorf("spillover ID %q not found — no live spillovers for this session (they are released when the session ends and do not survive a gateway restart); re-run the tool that produced the large output",
			spillID)
	}
	if entry.SessionKey != sessionKey {
		return "", fmt.Errorf("spillover ID %q belongs to a different session", spillID)
	}

	data, err := os.ReadFile(entry.Path)
	if err != nil {
		return "", fmt.Errorf("spillover read %q: %w", spillID, err)
	}
	return string(data), nil
}

// IsExternalOriginTool reports whether a tool's PRIMARY job is fetching content
// from outside the operator's trust boundary — a web page, an email body, an
// attacker-craftable image — i.e. text an attacker could have authored.
//
// This is the single source of truth for that classification: Store consults it
// so a spill is marked from its producing tool's name no matter which call site
// wrote it, and the chat layer's untrusted-tool gate delegates to it for live
// reads. Deliberately narrow — operator-owned reads (wiki, files, office docs,
// calendar, contacts, phone, groupware) are excluded because tainting them
// would over-block common turns.
func IsExternalOriginTool(name string) bool {
	switch name {
	case "web", "browse", "browser", "research_panel", "watch", "mail_archive", "ocr":
		return true
	default:
		return false
	}
}

// MarkExternalOrigin records that a spill holds content sourced from outside
// the operator's trust boundary.
//
// Store already marks spills whose producing tool is externally classified, so
// this is for the INDIRECT case only: code_action reads mail or web pages
// through its bridge and spills them under its own name, which no name-based
// rule can catch.
func (s *SpilloverStore) MarkExternalOrigin(spillID string) {
	s.mu.Lock()
	if entry, ok := s.index[spillID]; ok {
		entry.ExternalOrigin = true
	}
	s.mu.Unlock()
}

// IsExternalOrigin reports whether a spill holds externally sourced content.
//
// The untrusted-origin gate uses it to decide whether reading a spill should
// taint the turn: a spill created by `web` or `mail_archive` carries the same
// attacker-authored text on turn N+5 as it did on turn N, and now that spills
// survive their producing run, that later read has to taint exactly like the
// original fetch did. Unknown IDs and cross-session reads report false — those
// reads fail anyway.
func (s *SpilloverStore) IsExternalOrigin(spillID, sessionKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.index[spillID]
	return ok && entry.SessionKey == sessionKey && entry.ExternalOrigin
}

// FormatPreview builds the compact preview string inserted into the LLM context.
//
// The preview text flows back into the model context and subsequently into
// the transcript, so it is redacted here as well. Redaction is idempotent —
// even if Store already masked the content, running it again on the head/tail
// slices is safe and cheap thanks to the mightContainSecret prefilter.
func FormatPreview(spillID, toolName, content string) string {
	content = redact.String(content)
	origLen := len(content)

	head := content
	if len(head) > PreviewHeadChars {
		head = head[:PreviewHeadChars]
	}

	tail := ""
	if origLen > PreviewHeadChars+PreviewTailChars {
		tail = content[origLen-PreviewTailChars:]
	} else if origLen > PreviewHeadChars {
		tail = content[PreviewHeadChars:]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[SpillOver: ID=%s | %s | %d chars · %d lines]\n",
		spillID, toolName, origLen, strings.Count(content, "\n")+1)
	fmt.Fprintf(&sb, "--- Preview (first %d chars) ---\n", len(head))
	sb.WriteString(head)
	sb.WriteByte('\n')
	if tail != "" {
		fmt.Fprintf(&sb, "--- Preview (last %d chars) ---\n", len(tail))
		sb.WriteString(tail)
		sb.WriteByte('\n')
	}
	fmt.Fprintf(&sb, "To read full content, use tool: read_spillover(\"%s\")", spillID)
	return sb.String()
}

// SpillAndPreview is a convenience method: Store + FormatPreview.
// If storing fails the original output is returned unchanged (degradation, not failure).
func (s *SpilloverStore) SpillAndPreview(sessionKey, toolName, output string) string {
	spillID, err := s.Store(sessionKey, toolName, output)
	if err != nil {
		slog.Warn("spillover store failed, returning raw output", "tool", toolName, "err", err)
		return output
	}
	return FormatPreview(spillID, toolName, output)
}

// CleanSession removes all spilled files belonging to sessionKey that are
// tracked in the in-memory index. Use RemoveSession for a stronger cleanup
// that also sweeps orphan files left on disk (e.g. after a crash/restart).
func (s *SpilloverStore) CleanSession(sessionKey string) {
	s.mu.Lock()
	var toDelete []string
	for id, entry := range s.index {
		if entry.SessionKey == sessionKey {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		entry := s.index[id]
		os.Remove(entry.Path)
		delete(s.index, id)
	}
	s.mu.Unlock()
}

// RemoveSession removes every spill file belonging to sessionKey, both the
// entries tracked in the in-memory index and any orphan files on disk whose
// filename prefix matches the sanitized session key (e.g. left over from a
// previous process that crashed before index cleanup ran). Idempotent: no
// error if the base directory does not exist yet.
//
// Called from the session lifecycle subscriber (see
// server_spillover_lifecycle.go) on terminal/reset/delete events so abandoned
// spillover files are reclaimed as soon as the session ends instead of
// waiting for the 30-minute TTL sweep.
func (s *SpilloverStore) RemoveSession(sessionKey string) error {
	if sessionKey == "" {
		return nil
	}

	// 1. Drop in-memory entries and delete their files.
	s.CleanSession(sessionKey)

	// 2. Sweep filesystem for orphan files belonging to exactly this session.
	//    The prefix carries a hash of the FULL key, so a parent key cannot
	//    match its children: session keys nest ("client:main" is the parent of
	//    the native per-conversation chats "client:main:<uuid>"), and a plain
	//    sanitized-name prefix would let a parent's cleanup delete every live
	//    child's spills while their index entries survived — leaving
	//    read_spillover pointing at files that no longer exist.
	prefix := sessionFilePrefix(sessionKey) + "_"
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("spillover readdir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(s.baseDir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("spillover remove %q: %w", name, err)
		}
	}
	return nil
}

// StartCleanup runs a background goroutine that removes expired spill files
// every cleanupInterval.  It stops when ctx is cancelled.
func (s *SpilloverStore) StartCleanup(ctx context.Context) {
	// safego: a panic in cleanExpired (filesystem edge cases) must not kill
	// the process; the loop exits via ctx on shutdown.
	safego.GoWithSlog(slog.Default(), "spillover-cleanup", func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanExpired()
			}
		}
	})
}

// cleanExpired removes aged-out entries whose session is already gone.
//
// Age alone is not sufficient grounds to delete: compaction stubs older tool
// results down to a read_spillover pointer and tells the model the full output
// is still available, so a spill under a live session must outlive the TTL no
// matter how long it sits untouched. Reclaiming a live session's spills is the
// session-end hook's job (RemoveSession); this sweep only collects orphans —
// spills whose session ended without that hook running (crash, restart).
//
// The liveness predicate runs with s.mu released (concurrency.md §3): it calls
// into the session manager, and holding s.mu across it would nest an unrelated
// lock under ours.
func (s *SpilloverStore) cleanExpired() {
	now := time.Now()

	type aged struct {
		id, sessionKey, path string
	}
	var candidates []aged
	s.mu.RLock()
	for id, entry := range s.index {
		if now.Sub(entry.CreatedAt) > SpilloverTTL {
			candidates = append(candidates, aged{id: id, sessionKey: entry.SessionKey, path: entry.Path})
		}
	}
	s.mu.RUnlock()

	// Orphan files: the index lives only in memory, so every spill written by a
	// previous process is invisible above. Now that a completed run no longer
	// releases spills, those files would otherwise accumulate across restarts
	// forever — the orphan collection this sweep advertises has to actually
	// look at the disk.
	s.sweepUnindexedFiles(now)

	if len(candidates) == 0 {
		return
	}

	// Evaluate liveness once per session, outside the lock.
	live := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if _, seen := live[c.sessionKey]; !seen {
			live[c.sessionKey] = s.isSessionAlive(c.sessionKey)
		}
	}

	s.mu.Lock()
	for _, c := range candidates {
		if live[c.sessionKey] {
			continue // session still running — its spill handles must stay valid
		}
		// Re-check: Store may have replaced the entry while the lock was down.
		if entry, ok := s.index[c.id]; !ok || entry.Path != c.path {
			continue
		}
		os.Remove(c.path)
		delete(s.index, c.id)
	}
	s.mu.Unlock()
}

// sweepUnindexedFiles removes spill files on disk that no index entry claims and
// that are older than the TTL.
//
// A file is unindexed only if this process did not write it (the index is
// rebuilt empty on start), so its session cannot be live here. The age bound
// still applies: it keeps the sweep from racing a file another writer is in the
// middle of creating.
func (s *SpilloverStore) sweepUnindexedFiles(now time.Time) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return // not created yet, or unreadable — nothing to collect
	}

	s.mu.RLock()
	known := make(map[string]struct{}, len(s.index))
	for _, e := range s.index {
		known[e.Path] = struct{}{}
	}
	s.mu.RUnlock()

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(s.baseDir, e.Name())
		if _, ok := known[path]; ok {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= SpilloverTTL {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("spillover orphan sweep failed", "file", e.Name(), "err", err)
		}
	}
}

// --- helpers ---

// enforceSessionQuotaLocked drops this session's oldest spills until it is back
// under the count and byte caps, returning the paths whose files the caller
// must delete once the lock is released.
//
// keepID is exempt from eviction: Store calls this right after inserting, and
// evicting that entry would hand the caller a handle whose file is already
// gone — capToolOutput would embed a dead read_spillover pointer and the
// truncated middle would be unrecoverable. Timestamps come from before the
// disk write, so a concurrent spill can otherwise look older than it is.
//
// Caller must hold s.mu. File removal is deliberately left to the caller: disk
// I/O under the index lock would stall every concurrent Store and Load.
func (s *SpilloverStore) enforceSessionQuotaLocked(sessionKey, keepID string) []string {
	type aged struct {
		id      string
		entry   *spillEntry
		created time.Time
	}
	var owned []aged
	total := 0
	for id, e := range s.index {
		if e.SessionKey != sessionKey {
			continue
		}
		total += e.OrigLen
		if id == keepID {
			continue // never evict the spill we are about to hand back a handle for
		}
		owned = append(owned, aged{id: id, entry: e, created: e.CreatedAt})
	}
	// owned excludes keepID, so count it back in when measuring the session.
	count := len(owned)
	if keepID != "" {
		count++
	}
	if count <= maxSpillsPerSession && total <= maxSpillBytesPerSession {
		return nil
	}

	sort.Slice(owned, func(i, j int) bool { return owned[i].created.Before(owned[j].created) })

	var paths []string
	for _, o := range owned {
		if count <= maxSpillsPerSession && total <= maxSpillBytesPerSession {
			break
		}
		paths = append(paths, o.entry.Path)
		total -= o.entry.OrigLen
		count--
		delete(s.index, o.id)
	}
	return paths
}

// sessionFilePrefix builds the filename prefix that identifies a spill's owning
// session unambiguously: the sanitized key (readable) plus a short hash of the
// FULL key (exact).
//
// The sanitized name alone is ambiguous because keys nest and ":" collapses to
// "_": "client:main" and "client:main:<uuid>" both sanitize to names starting
// "client_main_". The hash is taken over the untouched key, so no parent prefix
// can match a child's files.
func sessionFilePrefix(sessionKey string) string {
	sum := sha256.Sum256([]byte(sessionKey))
	return fmt.Sprintf("%s_%x", sanitizeSessionKey(sessionKey), sum[:sessionHashBytes])
}

// sanitizeSessionKey replaces characters unsafe for filenames.
func sanitizeSessionKey(key string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_")
	return r.Replace(key)
}

// sanitizeToolName keeps only alphanumeric and underscore.
func sanitizeToolName(name string) string {
	var sb strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			sb.WriteRune(ch)
		}
	}
	if sb.Len() == 0 {
		return "tool"
	}
	return sb.String()
}
