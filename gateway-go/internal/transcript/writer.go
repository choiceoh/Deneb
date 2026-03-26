// Package transcript implements append-only JSONL session transcript persistence.
//
// Session transcripts are stored as newline-delimited JSON files, one message
// per line. The first line is always a session header. This mirrors the
// TypeScript SessionManager transcript format from src/config/sessions/transcript.ts.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// AppendListener is called when a message is appended to a session transcript.
type AppendListener func(sessionKey string, msg json.RawMessage)

// Writer manages session transcript files.
type Writer struct {
	mu        sync.Mutex
	baseDir   string // e.g. ~/.deneb/agents/<agentId>/sessions/
	logger    *slog.Logger
	known     map[string]bool // tracks which sessions have been initialized
	listeners []AppendListener
	listMu    sync.RWMutex
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
// Returns an error if the session key contains unsafe path characters.
func (w *Writer) SessionPath(sessionKey string) (string, error) {
	if err := validateSessionKey(sessionKey); err != nil {
		return "", err
	}
	return filepath.Join(w.baseDir, sessionKey+".jsonl"), nil
}

// DeleteSession removes the transcript file and clears the known-session cache
// for the given key. Returns nil if the file does not exist.
func (w *Writer) DeleteSession(sessionKey string) error {
	path, err := w.SessionPath(sessionKey)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.known, sessionKey)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("transcript: delete session %q: %w", sessionKey, err)
	}
	return nil
}

// validateSessionKey rejects session keys that could cause path traversal.
func validateSessionKey(key string) error {
	if key == "" {
		return fmt.Errorf("transcript: empty session key")
	}
	if strings.Contains(key, "..") || strings.ContainsAny(key, "/\\") {
		return fmt.Errorf("transcript: unsafe session key: %q", key)
	}
	return nil
}

// EnsureSession creates the transcript file with a header if it does not
// already exist. If the file already exists, this is a no-op.
func (w *Writer) EnsureSession(sessionKey string, header SessionHeader) error {
	if err := validateSessionKey(sessionKey); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.known[sessionKey] {
		return nil
	}

	path := filepath.Join(w.baseDir, sessionKey+".jsonl")

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
	if err := validateSessionKey(sessionKey); err != nil {
		return err
	}
	if !json.Valid(msg) {
		return fmt.Errorf("transcript: invalid JSON message")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	path := filepath.Join(w.baseDir, sessionKey+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("transcript: open: %w", err)
	}
	defer f.Close()

	// Build the line as a new slice to avoid mutating the caller's msg.
	line := make([]byte, len(msg)+1)
	copy(line, msg)
	line[len(msg)] = '\n'

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("transcript: write: %w", err)
	}

	// Notify listeners of the new message.
	w.notifyListeners(sessionKey, msg)

	return nil
}

// PreviewItem is a lightweight representation of a transcript message for previews.
type PreviewItem struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// ReadPreview reads the last maxItems non-header messages from a session transcript.
// Returns an empty slice (not error) if the transcript file does not exist.
func (w *Writer) ReadPreview(sessionKey string, maxItems int) ([]PreviewItem, error) {
	path, err := w.SessionPath(sessionKey)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("transcript: open preview: %w", err)
	}
	defer f.Close()

	// Read all non-header messages into a ring buffer of maxItems capacity.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 10*1024*1024) // 10 MB max line
	first := true
	var ring []PreviewItem

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			continue // Skip header.
		}

		var msg struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Type      string `json:"type"`
			Timestamp int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // Skip malformed lines.
		}

		item := PreviewItem{
			Role:      msg.Role,
			Content:   msg.Content,
			Type:      msg.Type,
			Timestamp: msg.Timestamp,
		}
		// Truncate long content for preview.
		if len(item.Content) > 500 {
			item.Content = item.Content[:497] + "..."
		}

		ring = append(ring, item)
		if len(ring) > maxItems {
			ring = ring[1:]
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("transcript: scan preview: %w", err)
	}

	if ring == nil {
		return []PreviewItem{}, nil
	}
	return ring, nil
}

// AppendStructured marshals a value to JSON and appends it to the transcript.
func (w *Writer) AppendStructured(sessionKey string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("transcript: marshal: %w", err)
	}
	return w.AppendMessage(sessionKey, data)
}

// OnAppend registers a listener that is called after each successful message append.
// Returns an unsubscribe function.
func (w *Writer) OnAppend(fn AppendListener) func() {
	w.listMu.Lock()
	w.listeners = append(w.listeners, fn)
	idx := len(w.listeners) - 1
	w.listMu.Unlock()

	return func() {
		w.listMu.Lock()
		defer w.listMu.Unlock()
		if idx < len(w.listeners) {
			// Set to nil instead of removing to preserve indices.
			w.listeners[idx] = nil
		}
	}
}

// notifyListeners calls all registered append listeners.
func (w *Writer) notifyListeners(sessionKey string, msg json.RawMessage) {
	w.listMu.RLock()
	defer w.listMu.RUnlock()
	for _, fn := range w.listeners {
		if fn != nil {
			fn(sessionKey, msg)
		}
	}
}

// ReadMessages reads all non-header messages from a session transcript.
// Returns the full raw JSON messages (unlike ReadPreview which truncates).
func (w *Writer) ReadMessages(sessionKey string) ([]json.RawMessage, error) {
	path, err := w.SessionPath(sessionKey)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []json.RawMessage{}, nil
		}
		return nil, fmt.Errorf("transcript: open: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 10*1024*1024) // 10 MB max line
	first := true
	var messages []json.RawMessage

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			continue // Skip header.
		}
		// Make a copy since scanner reuses the buffer.
		msg := make(json.RawMessage, len(line))
		copy(msg, line)
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("transcript: scan: %w", err)
	}

	if messages == nil {
		return []json.RawMessage{}, nil
	}
	return messages, nil
}
