// Session transcript compression: reduces JSONL transcript size by extracting
// and summarizing older messages while preserving recent conversation context.
//
// This mirrors the compression logic from the TypeScript codebase that uses
// Aurora context engine for compacting session transcripts.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// CompactionConfig controls transcript compression behavior.
type CompactionConfig struct {
	// ContextThreshold is the ratio of tokens used before triggering compaction (0.0-1.0).
	ContextThreshold float64 `json:"contextThreshold"`
	// FreshTailCount is the number of recent messages to always preserve uncompacted.
	FreshTailCount int `json:"freshTailCount"`
	// MaxUncompactedMessages triggers compaction when exceeded (0 = no limit).
	MaxUncompactedMessages int `json:"maxUncompactedMessages"`
}

// DefaultCompactionConfig returns production defaults for transcript compression.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		ContextThreshold:       0.75,
		FreshTailCount:         8,
		MaxUncompactedMessages: 200,
	}
}

// CompactionResult reports the outcome of a transcript compaction.
type CompactionResult struct {
	OK               bool   `json:"ok"`
	Compacted        bool   `json:"compacted"`
	Reason           string `json:"reason,omitempty"`
	OriginalMessages int    `json:"originalMessages"`
	RetainedMessages int    `json:"retainedMessages"`
	SummaryCount     int    `json:"summaryCount"`
}

// TranscriptMessage is a parsed message from a JSONL transcript line.
type TranscriptMessage struct {
	Type       string `json:"type,omitempty"`
	Role       string `json:"role,omitempty"`
	Content    string `json:"content,omitempty"`
	ID         string `json:"id,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
	TokenCount int    `json:"tokenCount,omitempty"`
	// Raw preserves the original JSON for re-serialization.
	Raw json.RawMessage `json:"-"`
}

// Compressor handles transcript compression operations.
type Compressor struct {
	mu     sync.Mutex
	config CompactionConfig
	logger *slog.Logger
}

// NewCompressor creates a new transcript compressor with the given config.
func NewCompressor(config CompactionConfig, logger *slog.Logger) *Compressor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Compressor{
		config: config,
		logger: logger,
	}
}

// ShouldCompact evaluates whether a session transcript needs compaction.
func (c *Compressor) ShouldCompact(sessionKey string, w *Writer) (bool, string) {
	path, err := w.SessionPath(sessionKey)
	if err != nil {
		return false, ""
	}

	messages, err := c.readMessages(path)
	if err != nil {
		return false, ""
	}

	msgCount := len(messages)

	// Check message count threshold.
	if c.config.MaxUncompactedMessages > 0 && msgCount > c.config.MaxUncompactedMessages {
		return true, fmt.Sprintf("message count %d exceeds threshold %d", msgCount, c.config.MaxUncompactedMessages)
	}

	// Check token ratio if token counts are available.
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}
	if totalTokens > 0 {
		// Rough estimate: context window of ~100k tokens.
		ratio := float64(totalTokens) / 100_000
		if ratio > c.config.ContextThreshold {
			return true, fmt.Sprintf("token ratio %.2f exceeds threshold %.2f", ratio, c.config.ContextThreshold)
		}
	}

	return false, ""
}

// Compact performs transcript compaction by retaining the header + fresh tail
// messages and producing a compressed file. Older messages are summarized into
// a single summary entry.
//
// This is a basic compaction strategy; the full Aurora sweep engine in core-rs
// handles the advanced hierarchical compaction via FFI.
func (c *Compressor) Compact(sessionKey string, w *Writer) (*CompactionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	path, err := w.SessionPath(sessionKey)
	if err != nil {
		return nil, err
	}

	messages, err := c.readMessages(path)
	if err != nil {
		return nil, fmt.Errorf("transcript compact: read: %w", err)
	}

	if len(messages) <= c.config.FreshTailCount {
		return &CompactionResult{
			OK:               true,
			Compacted:        false,
			Reason:           "below threshold",
			OriginalMessages: len(messages),
			RetainedMessages: len(messages),
		}, nil
	}

	// Split into head (to compact) and tail (to preserve).
	splitIdx := len(messages) - c.config.FreshTailCount
	head := messages[:splitIdx]
	tail := messages[splitIdx:]

	// Build summary from head messages.
	summary := buildSummary(head)

	// Read header line (first line of file).
	header, err := readFirstLine(path)
	if err != nil {
		return nil, fmt.Errorf("transcript compact: header: %w", err)
	}

	// Write compacted file.
	tmpPath := path + ".compact.tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("transcript compact: create tmp: %w", err)
	}
	defer f.Close()

	bw := bufio.NewWriter(f)

	// Write header.
	bw.WriteString(header)
	bw.WriteByte('\n')

	// Write summary entry.
	summaryJSON, _ := json.Marshal(summary)
	bw.Write(summaryJSON)
	bw.WriteByte('\n')

	// Write preserved tail.
	for _, msg := range tail {
		bw.Write(msg.Raw)
		bw.WriteByte('\n')
	}

	if err := bw.Flush(); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("transcript compact: flush: %w", err)
	}
	f.Close()

	// Atomic replace.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("transcript compact: rename: %w", err)
	}

	c.logger.Info("transcript compacted",
		"session", sessionKey,
		"original", len(messages),
		"retained", len(tail),
		"summarized", len(head),
	)

	return &CompactionResult{
		OK:               true,
		Compacted:        true,
		Reason:           "threshold exceeded",
		OriginalMessages: len(messages),
		RetainedMessages: len(tail),
		SummaryCount:     1,
	}, nil
}

// readMessages reads all non-header messages from a JSONL transcript file.
func (c *Compressor) readMessages(path string) ([]TranscriptMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []TranscriptMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 10*1024*1024) // 10 MB max line
	first := true

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			continue // Skip header.
		}

		dec := json.NewDecoder(bytes.NewReader(line))
		for {
			raw := make([]byte, len(line))
			copy(raw, line)

			var msg TranscriptMessage
			if err := dec.Decode(&msg); err != nil {
				if err != io.EOF {
					// skip malformed tail
				}
				break
			}
			msg.Raw = json.RawMessage(raw)
			messages = append(messages, msg)
		}
	}

	return messages, scanner.Err()
}

// readFirstLine reads the first non-empty line from a file.
func readFirstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("empty file")
}

// buildSummary creates a summary message from a set of transcript messages.
func buildSummary(messages []TranscriptMessage) map[string]any {
	var parts []string
	for _, msg := range messages {
		if msg.Content == "" {
			continue
		}
		prefix := msg.Role
		if prefix == "" {
			prefix = "system"
		}
		// Truncate long messages.
		content := msg.Content
		if len(content) > 500 {
			content = content[:497] + "..."
		}
		parts = append(parts, fmt.Sprintf("[%s] %s", prefix, content))
	}

	summary := map[string]any{
		"type":      "summary",
		"role":      "system",
		"content":   fmt.Sprintf("Summary of %d earlier messages:\n%s", len(messages), strings.Join(parts, "\n")),
		"timestamp": time.Now().UnixMilli(),
		"metadata": map[string]any{
			"compacted":     true,
			"originalCount": len(messages),
			"compactedAt":   time.Now().UnixMilli(),
		},
	}
	return summary
}
