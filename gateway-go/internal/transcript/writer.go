// Package transcript implements append-only JSONL session transcript persistence.
//
// Session transcripts are stored as newline-delimited JSON files, one message
// per line. The first line is always a session header. This mirrors the
// TypeScript SessionManager transcript format from src/config/sessions/transcript.ts.
package transcript

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionHeader is the first line of a session transcript file.
type SessionHeader struct {
	Type      string `json:"type"`      // always "session"
	Version   int    `json:"version"`   // transcript format version
	ID        string `json:"id"`        // session identifier
	Timestamp int64  `json:"timestamp"` // creation time (unix ms)
	Cwd       string `json:"cwd,omitempty"`
}

// Writer manages session transcript files.
type Writer struct {
	mu      sync.Mutex
	baseDir string // e.g. ~/.deneb/agents/<agentId>/sessions/
	logger  *slog.Logger
	known   map[string]bool // tracks which sessions have been initialized
}

// NewWriter creates a new transcript writer.
// baseDir is the directory where session JSONL files are stored.
func NewWriter(baseDir string, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		baseDir: baseDir,
		logger:  logger,
		known:   make(map[string]bool),
	}
}

// SessionPath returns the file path for a session's transcript.
func (w *Writer) SessionPath(sessionKey string) string {
	return filepath.Join(w.baseDir, sessionKey+".jsonl")
}

// EnsureSession creates the transcript file with a header if it does not
// already exist. If the file already exists, this is a no-op.
func (w *Writer) EnsureSession(sessionKey string, header SessionHeader) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.known[sessionKey] {
		return nil
	}

	path := w.SessionPath(sessionKey)

	// Check if file already exists.
	if _, err := os.Stat(path); err == nil {
		w.known[sessionKey] = true
		return nil
	}

	// Create directory tree.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("transcript: mkdir: %w", err)
	}

	// Write header as first line.
	header.Type = "session"
	if header.Timestamp == 0 {
		header.Timestamp = time.Now().UnixMilli()
	}

	data, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("transcript: marshal header: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("transcript: write header: %w", err)
	}

	w.known[sessionKey] = true
	w.logger.Debug("session transcript created", "session", sessionKey, "path", path)
	return nil
}

// AppendMessage appends a message to the session transcript.
// The message is written as a single JSON line followed by a newline.
// The session file must already exist (call EnsureSession first).
func (w *Writer) AppendMessage(sessionKey string, msg json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	path := w.SessionPath(sessionKey)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("transcript: open: %w", err)
	}
	defer f.Close()

	// Ensure the message is valid JSON and compact.
	var compact json.RawMessage
	if json.Valid(msg) {
		compact = msg
	} else {
		return fmt.Errorf("transcript: invalid JSON message")
	}

	line := append(compact, '\n')
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("transcript: write: %w", err)
	}

	return nil
}

// AppendStructured marshals a value to JSON and appends it to the transcript.
func (w *Writer) AppendStructured(sessionKey string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("transcript: marshal: %w", err)
	}
	return w.AppendMessage(sessionKey, data)
}
