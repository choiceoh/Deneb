package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Type aliases — canonical interface and result types are in toolctx/.
type SearchResult = toolctx.SearchResult
type MatchedMsg = toolctx.MatchedMsg
type TranscriptStore = toolctx.TranscriptStore

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
		dec := json.NewDecoder(bytes.NewReader(line))
		for {
			var msg ChatMessage
			if err := dec.Decode(&msg); err != nil {
				if err != io.EOF {
					// skip malformed tail
				}
				break
			}
			all = append(all, msg)
		}
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

// CloneRecent copies the most recent `limit` messages from srcKey to dstKey.
func (s *FileTranscriptStore) CloneRecent(srcKey, dstKey string, limit int) error {
	msgs, _, err := s.Load(srcKey, limit)
	if err != nil {
		return fmt.Errorf("clone: load source %q: %w", srcKey, err)
	}
	if len(msgs) == 0 {
		return nil
	}

	// Write all messages to destination in one batch (more efficient than per-message Append).
	s.mu.Lock()
	defer s.mu.Unlock()

	dstPath := s.sessionPath(dstKey)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("clone: create dir: %w", err)
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("clone: create dst: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, msg := range msgs {
		data, merr := json.Marshal(msg)
		if merr != nil {
			continue
		}
		_, _ = w.Write(data)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("clone: flush: %w", err)
	}
	return nil
}

// AppendSystemNote appends a system-role message with the given text.
func (s *FileTranscriptStore) AppendSystemNote(sessionKey, text string) error {
	return s.Append(sessionKey, NewTextChatMessage("system", text, 0))
}

// ListKeys returns all session keys by scanning JSONL files in baseDir.
func (s *FileTranscriptStore) ListKeys() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list transcript keys: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") {
			keys = append(keys, strings.TrimSuffix(name, ".jsonl"))
		}
	}
	return keys, nil
}

// searchMessages finds messages matching queryLower in msgs, consuming from *remaining.
func searchMessages(msgs []ChatMessage, queryLower string, remaining *int) []MatchedMsg {
	var matches []MatchedMsg
	for i, msg := range msgs {
		if *remaining <= 0 {
			break
		}
		if strings.Contains(strings.ToLower(msg.TextContent()), queryLower) {
			m := MatchedMsg{Index: i, Message: msg}
			if i > 0 {
				m.Context = append(m.Context, msgs[i-1])
			}
			if i < len(msgs)-1 {
				m.Context = append(m.Context, msgs[i+1])
			}
			matches = append(matches, m)
			*remaining--
		}
	}
	return matches
}

// Search scans all transcripts for messages containing the query (case-insensitive).
// Returns up to maxResults matching messages grouped by session key.
func (s *FileTranscriptStore) Search(query string, maxResults int) ([]SearchResult, error) {
	keys, err := s.ListKeys()
	if err != nil {
		return nil, err
	}
	queryLower := strings.ToLower(query)
	var results []SearchResult
	remaining := maxResults

	for _, key := range keys {
		if remaining <= 0 {
			break
		}
		msgs, _, err := s.Load(key, 0)
		if err != nil || len(msgs) == 0 {
			continue
		}
		if matches := searchMessages(msgs, queryLower, &remaining); len(matches) > 0 {
			results = append(results, SearchResult{SessionKey: key, Matches: matches})
		}
	}
	return results, nil
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

// ListKeys returns all session keys.
func (s *MemoryTranscriptStore) ListKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.sessions))
	for k := range s.sessions {
		keys = append(keys, k)
	}
	return keys, nil
}

// CloneRecent copies the most recent `limit` messages from srcKey to dstKey.
func (s *MemoryTranscriptStore) CloneRecent(srcKey, dstKey string, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.sessions[srcKey]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	dst := make([]ChatMessage, len(msgs))
	copy(dst, msgs)
	s.sessions[dstKey] = dst
	return nil
}

// Search scans all in-memory transcripts for messages containing the query.
func (s *MemoryTranscriptStore) Search(query string, maxResults int) ([]SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queryLower := strings.ToLower(query)
	var results []SearchResult
	remaining := maxResults

	for key, msgs := range s.sessions {
		if remaining <= 0 {
			break
		}
		if matches := searchMessages(msgs, queryLower, &remaining); len(matches) > 0 {
			results = append(results, SearchResult{SessionKey: key, Matches: matches})
		}
	}
	return results, nil
}
