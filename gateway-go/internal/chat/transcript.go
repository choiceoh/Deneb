package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TranscriptStore loads and persists session transcripts.
type TranscriptStore interface {
	// Load returns up to limit messages for the given session, plus the total count.
	Load(sessionKey string, limit int) ([]ChatMessage, int, error)
	// Append adds a message to the session transcript.
	Append(sessionKey string, msg ChatMessage) error
	// Delete removes the transcript for a session (used by /reset).
	Delete(sessionKey string) error
}

// FileTranscriptStore stores transcripts as JSONL files on disk.
// Each session gets a file at {baseDir}/{sessionKey}.jsonl.
type FileTranscriptStore struct {
	baseDir string
	mu      sync.Mutex // Serializes writes per-store (could be per-session for higher throughput).
}

// NewFileTranscriptStore creates a file-based transcript store.
func NewFileTranscriptStore(baseDir string) *FileTranscriptStore {
	return &FileTranscriptStore{baseDir: baseDir}
}

func (s *FileTranscriptStore) sessionPath(sessionKey string) string {
	// Sanitize session key for filesystem safety.
	safe := filepath.Base(sessionKey)
	return filepath.Join(s.baseDir, safe+".jsonl")
}

// Load reads messages from the JSONL file, returning the most recent `limit`.
func (s *FileTranscriptStore) Load(sessionKey string, limit int) ([]ChatMessage, int, error) {
	path := s.sessionPath(sessionKey)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	var all []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg ChatMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // Skip malformed lines.
		}
		all = append(all, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("read transcript: %w", err)
	}

	total := len(all)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, total, nil
}

// Append writes a message to the end of the JSONL file.
func (s *FileTranscriptStore) Append(sessionKey string, msg ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.sessionPath(sessionKey)

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open transcript for append: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

// Delete removes the transcript file for a session.
func (s *FileTranscriptStore) Delete(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.sessionPath(sessionKey)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete transcript: %w", err)
	}
	return nil
}

// MemoryTranscriptStore is an in-memory transcript store for testing.
type MemoryTranscriptStore struct {
	mu       sync.Mutex
	sessions map[string][]ChatMessage
}

// NewMemoryTranscriptStore creates an in-memory transcript store.
func NewMemoryTranscriptStore() *MemoryTranscriptStore {
	return &MemoryTranscriptStore{
		sessions: make(map[string][]ChatMessage),
	}
}

// Load returns messages for the session.
func (s *MemoryTranscriptStore) Load(sessionKey string, limit int) ([]ChatMessage, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.sessions[sessionKey]
	total := len(msgs)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	// Return a copy.
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	return out, total, nil
}

// Append adds a message.
func (s *MemoryTranscriptStore) Append(sessionKey string, msg ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionKey] = append(s.sessions[sessionKey], msg)
	return nil
}

// Delete removes transcript for a session.
func (s *MemoryTranscriptStore) Delete(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionKey)
	return nil
}
