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
)

// spillEntry tracks a single spilled result on disk.
type spillEntry struct {
	Path       string
	SessionKey string
	ToolName   string
	OrigLen    int
	CreatedAt  time.Time
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

	safeSess := sanitizeSessionKey(sessionKey)
	safeTool := sanitizeToolName(toolName)
	filename := fmt.Sprintf("%s_%d_%s_%s.txt", safeSess, now.UnixMilli(), safeTool, spillID)
	path := filepath.Join(s.baseDir, filename)

	persisted := redact.String(content)
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
	}
	s.mu.Unlock()

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

	// 2. Sweep filesystem for any orphan files with this session prefix.
	//    Filenames follow the pattern: <safeSess>_<ts>_<tool>_<id>.txt so an
	//    exact prefix match on "<safeSess>_" is a safe lower bound; we still
	//    guard against accidentally deleting a different session whose key
	//    happens to start with the same sanitized bytes by requiring the
	//    trailing underscore.
	prefix := sanitizeSessionKey(sessionKey) + "_"
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

// --- helpers ---

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
