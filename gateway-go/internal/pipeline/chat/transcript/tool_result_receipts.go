package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/pathutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

func (s *FileTranscriptStore) toolResultReceiptPath(sessionKey string) string {
	dir := pathutil.MustJoinUnder(s.baseDir, ".tool-results")
	return pathutil.MustJoinUnder(dir, pathutil.SafeFileName(sessionKey)+".jsonl")
}

// AppendToolResultReceipt durably records one completed tool call before the
// executor commits the ordered tool_result batch to the transcript.
func (s *FileTranscriptStore) AppendToolResultReceipt(
	sessionKey string,
	receipt chatport.ToolResultReceipt,
) error {
	if sessionKey == "" || receipt.ToolUseID == "" {
		return fmt.Errorf("append tool result receipt: session key and tool use ID are required")
	}
	receipt.Content = redact.String(receipt.Content)

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.toolResultReceiptPath(sessionKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create tool result receipt dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open tool result receipt for append: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal tool result receipt: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write tool result receipt: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync tool result receipt: %w", err)
	}
	return nil
}

// LoadToolResultReceipts returns every complete receipt record. A torn final
// write is ignored so a kill-9 cannot hide earlier completed calls.
func (s *FileTranscriptStore) LoadToolResultReceipts(sessionKey string) ([]chatport.ToolResultReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.toolResultReceiptPath(sessionKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open tool result receipts: %w", err)
	}
	defer f.Close()

	var receipts []chatport.ToolResultReceipt
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		var receipt chatport.ToolResultReceipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil || receipt.ToolUseID == "" {
			continue
		}
		receipts = append(receipts, receipt)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read tool result receipts: %w", err)
	}
	return receipts, nil
}

// DeleteToolResultReceipts removes ephemeral recovery state for a session.
func (s *FileTranscriptStore) DeleteToolResultReceipts(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.toolResultReceiptPath(sessionKey)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete tool result receipts: %w", err)
	}
	return nil
}

// AppendToolResultReceipt records a completed tool call in memory.
func (s *MemoryTranscriptStore) AppendToolResultReceipt(
	sessionKey string,
	receipt chatport.ToolResultReceipt,
) error {
	if sessionKey == "" || receipt.ToolUseID == "" {
		return fmt.Errorf("append tool result receipt: session key and tool use ID are required")
	}
	receipt.Content = redact.String(receipt.Content)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolResults[sessionKey] = append(s.toolResults[sessionKey], receipt)
	return nil
}

// LoadToolResultReceipts returns a defensive copy of in-memory receipts.
func (s *MemoryTranscriptStore) LoadToolResultReceipts(sessionKey string) ([]chatport.ToolResultReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts := s.toolResults[sessionKey]
	out := make([]chatport.ToolResultReceipt, len(receipts))
	copy(out, receipts)
	return out, nil
}

// DeleteToolResultReceipts removes in-memory recovery state.
func (s *MemoryTranscriptStore) DeleteToolResultReceipts(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.toolResults, sessionKey)
	return nil
}
