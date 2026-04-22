package agentlog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxLogBytes = 5_000_000 // 5 MB per session file
	keepLines   = 3_000
)

// Writer appends agent log entries to per-session JSONL files.
type Writer struct {
	mu      sync.Mutex
	baseDir string
}

// NewWriter creates a Writer that stores logs under baseDir.
// baseDir is typically ~/.deneb/agent-logs/.
func NewWriter(baseDir string) *Writer {
	return &Writer{baseDir: baseDir}
}

// logPath returns the JSONL file path for a session.
func (w *Writer) logPath(sessionKey string) string {
	safe := strings.ReplaceAll(strings.ReplaceAll(sessionKey, "/", ""), "\\", "")
	safe = strings.ReplaceAll(safe, "\x00", "")
	if safe == "" {
		safe = "_invalid_"
	}
	return filepath.Join(w.baseDir, safe+".jsonl")
}

// Append writes a log entry to the session's JSONL file.
func (w *Writer) Append(entry LogEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	path := w.logPath(entry.Session)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create agent log dir: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal agent log entry: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open agent log: %w", err)
	}
	_, writeErr := f.Write(append(data, '\n'))
	f.Close()
	if writeErr != nil {
		return fmt.Errorf("write agent log: %w", writeErr)
	}

	w.pruneIfNeeded(path)
	return nil
}

// pruneIfNeeded trims the file to keepLines if it exceeds maxLogBytes.
func (w *Writer) pruneIfNeeded(path string) {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() <= int64(maxLogBytes) {
		return
	}

	entries := readAllEntries(path)
	if len(entries) <= keepLines {
		return
	}

	kept := entries[len(entries)-keepLines:]
	var buf strings.Builder
	for _, e := range kept {
		data, _ := json.Marshal(e)
		buf.Write(data)
		buf.WriteByte('\n')
	}

	tmp := path + ".tmp"
	// Clean up any leftover tmp from a previously interrupted rotate.
	// Ignoring the error here is correct: if the file doesn't exist we're
	// happy; if removal fails, the subsequent WriteFile will overwrite it.
	_ = os.Remove(tmp)

	// Retry the tmp write + rename once. Most rotate failures are brief
	// filesystem hiccups; a single retry eliminates the vast majority of
	// spurious Warn logs without masking persistent hardware problems.
	const maxAttempts = 2
	var writeErr error
	for attempt := range maxAttempts {
		writeErr = os.WriteFile(tmp, []byte(buf.String()), 0o600)
		if writeErr == nil {
			break
		}
		if attempt+1 < maxAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if writeErr != nil {
		slog.Warn("agentlog: rotate write failed — log file not rotated",
			"path", path, "error", writeErr)
		return
	}

	var renameErr error
	for attempt := range maxAttempts {
		renameErr = os.Rename(tmp, path)
		if renameErr == nil {
			break
		}
		if attempt+1 < maxAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if renameErr != nil {
		slog.Warn("agentlog: rotate rename failed — tmp file remains",
			"tmp", tmp, "path", path, "error", renameErr)
		// Best-effort tmp cleanup so the next rotate doesn't trip on it.
		_ = os.Remove(tmp)
	}
}
