// Package agent — SpilloverStore saves large tool results to disk and returns
// compact previews for the LLM context window.
//
// When a tool result exceeds MaxResultChars the full content is written to
// ~/.deneb/spillover/{session}_{ts}_{tool}_{hash}.txt and replaced with a
// head+tail preview containing the spill ID.  The LLM can later retrieve the
// full content via the read_spillover tool.
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
)


// Spillover thresholds.
const (
	MaxResultChars   = 32 * 1024 // 32K chars — results larger than this are spilled
	PreviewHeadChars = 1024      // first 1K chars in preview
	PreviewTailChars = 1024      // last 1K chars in preview
	SpilloverTTL     = 30 * time.Minute
	cleanupInterval  = 10 * time.Minute
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
func (s *SpilloverStore) Store(sessionKey, toolName, content string) (string, error) {
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return "", fmt.Errorf("spillover mkdir: %w", err)
	}

	now := time.Now()
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", content[:min(256, len(content))], now.UnixNano(), sessionKey)))
	spillID := fmt.Sprintf("sp_%x", hash[:4]) // 8 hex chars

	safeSess := sanitizeSessionKey(sessionKey)
	safeTool := sanitizeToolName(toolName)
	filename := fmt.Sprintf("%s_%d_%s_%s.txt", safeSess, now.UnixMilli(), safeTool, spillID)
	path := filepath.Join(s.baseDir, filename)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("spillover write: %w", err)
	}

	s.mu.Lock()
	s.index[spillID] = &spillEntry{
		Path:       path,
		SessionKey: sessionKey,
		ToolName:   toolName,
		OrigLen:    len(content),
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
func FormatPreview(spillID, toolName string, content string) string {
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

// CleanSession removes all spilled files belonging to sessionKey.
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
