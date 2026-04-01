// Package coordinator provides shared infrastructure for coordinator mode,
// where a main agent orchestrates worker sub-agents through structured
// research → synthesis → implementation → verification phases.
package coordinator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// baseScratchpadDir is the root directory for all coordinator scratchpads.
func baseScratchpadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".deneb", "coordinator")
}

// ScratchpadDir returns the scratchpad directory for a coordinator session,
// creating it (and standard subdirectories) if it does not exist.
// The sessionID is sanitized to prevent path traversal.
func ScratchpadDir(sessionID string) (string, error) {
	safe := sanitizeSessionID(sessionID)
	if safe == "" {
		return "", fmt.Errorf("invalid session ID")
	}
	dir := filepath.Join(baseScratchpadDir(), safe)
	// Create the scratchpad with standard subdirectories.
	if err := os.MkdirAll(filepath.Join(dir, "implementation"), 0o755); err != nil {
		return "", fmt.Errorf("create scratchpad: %w", err)
	}
	return dir, nil
}

// ResolveScratchpadDir returns the scratchpad path for a session, creating it
// if needed. Returns an empty string on error (safe for prompt injection).
func ResolveScratchpadDir(sessionID string) string {
	dir, err := ScratchpadDir(sessionID)
	if err != nil {
		return ""
	}
	return dir
}

// CleanupScratchpad removes the scratchpad directory for a session.
func CleanupScratchpad(sessionID string) error {
	safe := sanitizeSessionID(sessionID)
	if safe == "" {
		return fmt.Errorf("invalid session ID")
	}
	dir := filepath.Join(baseScratchpadDir(), safe)
	return os.RemoveAll(dir)
}

// sanitizeSessionID makes a session ID safe for use as a directory name.
// Replaces path separators and colons with underscores, rejects empty strings.
func sanitizeSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	var b strings.Builder
	for _, c := range id {
		switch {
		case c == '/' || c == '\\' || c == ':' || c == '\x00':
			b.WriteRune('_')
		case c == '.':
			// Prevent ".." path traversal.
			b.WriteRune('_')
		default:
			b.WriteRune(c)
		}
	}
	result := b.String()
	if len(result) > 128 {
		result = result[:128]
	}
	return result
}
