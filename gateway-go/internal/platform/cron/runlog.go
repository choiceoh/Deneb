package cron

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// RunLogEntry is a single entry in the cron run log (JSONL format).
type RunLogEntry struct {
	Ts             int64  `json:"ts"` //nolint:staticcheck,revive // JSON wire format
	JobID          string `json:"jobId"`
	Action         string `json:"action"` // "finished"
	Status         string `json:"status,omitempty"`
	Error          string `json:"error,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Delivered      bool   `json:"delivered,omitempty"`
	DeliveryStatus string `json:"deliveryStatus,omitempty"`
	DeliveryError  string `json:"deliveryError,omitempty"`
	Retries        int    `json:"retries,omitempty"`
	SessionID      string `json:"sessionId,omitempty"`
	SessionKey     string `json:"sessionKey,omitempty"`
	RunAtMs        int64  `json:"runAtMs,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
	NextRunAtMs    int64  `json:"nextRunAtMs,omitempty"`
	Model          string `json:"model,omitempty"`
	Provider       string `json:"provider,omitempty"`
}

// RunLogPageResult holds a page of run log entries.
type RunLogPageResult struct {
	Entries    []RunLogEntry `json:"entries"`
	Total      int           `json:"total"`
	Offset     int           `json:"offset"`
	Limit      int           `json:"limit"`
	HasMore    bool          `json:"hasMore"`
	NextOffset *int          `json:"nextOffset,omitempty"`
}

const (
	DefaultRunLogMaxBytes  = 2_000_000 // 2 MB
	DefaultRunLogKeepLines = 2_000
)

// PersistentRunLog manages persistent JSONL run logs for cron jobs.
type PersistentRunLog struct {
	mu        sync.Mutex
	storePath string // base cron store path (e.g., ~/.deneb/cron/jobs.json)
	logger    *slog.Logger
}

// NewPersistentRunLog creates a new persistent run log manager.
func NewPersistentRunLog(storePath string) *PersistentRunLog {
	return &PersistentRunLog{storePath: storePath, logger: slog.Default()}
}

// SetLogger sets a custom logger for the run log.
func (rl *PersistentRunLog) SetLogger(l *slog.Logger) {
	if l != nil {
		rl.logger = l
	}
}

// runsDir returns the runs directory path.
func (rl *PersistentRunLog) runsDir() string {
	return filepath.Join(filepath.Dir(rl.storePath), "runs")
}

// logPath returns the log file path for a job.
func (rl *PersistentRunLog) logPath(jobID string) string {
	// Sanitize job ID to prevent path traversal.
	safe := strings.ReplaceAll(strings.ReplaceAll(jobID, "/", ""), "\\", "")
	safe = strings.ReplaceAll(safe, "\x00", "")
	if safe == "" {
		safe = "_invalid_"
	}
	return filepath.Join(rl.runsDir(), safe+".jsonl")
}

// Append adds a run log entry for a job and prunes if needed.
func (rl *PersistentRunLog) Append(entry RunLogEntry) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	logPath := rl.logPath(entry.JobID)

	// Ensure directory exists.
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create run log dir: %w", err)
	}

	data, err := json.Marshal(entry) //nolint:gosec // G117 false positive — not a secret
	if err != nil {
		return fmt.Errorf("marshal run log entry: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	_, writeErr := f.Write(append(data, '\n'))
	f.Close()
	if writeErr != nil {
		return fmt.Errorf("write run log: %w", writeErr)
	}

	// Prune if file is too large.
	rl.pruneIfNeeded(logPath)
	return nil
}

// ReadPage reads a page of run log entries for a job.
func (rl *PersistentRunLog) ReadPage(jobID string, opts RunLogReadOpts) RunLogPageResult {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return buildRunLogPage(readJSONLEntries(rl.logPath(jobID)), opts)
}

// RunLogReadOpts configures run log reading.
type RunLogReadOpts struct {
	Limit          int
	Offset         int
	Status         string // "all", "ok", "error", "skipped"
	DeliveryStatus string
	Query          string
	SortDir        string // "asc" or "desc"
}

func readJSONLEntries(path string) []RunLogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []RunLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		for {
			var entry RunLogEntry
			if err := dec.Decode(&entry); err != nil {
				break // skip malformed tail (or EOF)
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func (rl *PersistentRunLog) pruneIfNeeded(logPath string) {
	stat, err := os.Stat(logPath)
	if err != nil || stat.Size() <= int64(DefaultRunLogMaxBytes) {
		return
	}

	entries := readJSONLEntries(logPath)
	if len(entries) <= DefaultRunLogKeepLines {
		return
	}

	// Keep the most recent entries.
	kept := entries[len(entries)-DefaultRunLogKeepLines:]

	var buf strings.Builder
	for _, e := range kept {
		data, err := json.Marshal(e) //nolint:gosec // G117 false positive — not a secret
		if err != nil {
			rl.logger.Warn("failed to marshal run log entry during prune", "jobId", e.JobID, "error", err)
			continue
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	// Atomic write via temp file.
	tmp := logPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o600); err != nil {
		rl.logger.Warn("failed to write pruned run log", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, logPath); err != nil {
		rl.logger.Warn("failed to rename pruned run log", "from", tmp, "to", logPath, "error", err)
		// Clean up orphaned temp file.
		_ = os.Remove(tmp)
	}
}
