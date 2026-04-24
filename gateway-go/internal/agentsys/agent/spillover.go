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
package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
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
type SpilloverStore struct {
	baseDir string
	mu      sync.RWMutex
	index   map[string]*spillEntry // spill_id → metadata
}

// NewSpilloverStore creates a store rooted at baseDir (e.g. ~/.deneb/spillover).
// The directory is created lazily on the first Store call.
func NewSpilloverStore(baseDir string) *SpilloverStore {
	return &SpilloverStore{
		baseDir: baseDir,
		index:   make(map[string]*spillEntry),
	}
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
func (s *SpilloverStore) Load(spillID, sessionKey string) (string, error) {
	s.mu.RLock()
	entry, ok := s.index[spillID]
	s.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("spillover ID %q not found", spillID)
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
	fmt.Fprintf(&sb, "[SpillOver: ID=%s | %s | %d chars]\n", spillID, toolName, origLen)
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
	go func() {
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
	}()
}

// cleanExpired removes entries older than SpilloverTTL.
func (s *SpilloverStore) cleanExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.index {
		if now.Sub(entry.CreatedAt) > SpilloverTTL {
			os.Remove(entry.Path)
			delete(s.index, id)
		}
	}
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
