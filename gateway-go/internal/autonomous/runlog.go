package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// RunLogEntry represents a single cycle execution record.
type RunLogEntry struct {
	Timestamp   int64  `json:"ts"`
	Status      string `json:"status"`
	DurationMs  int64  `json:"durationMs"`
	GoalWorked  string `json:"goalWorked,omitempty"`
	Error       string `json:"error,omitempty"`
	UpdateCount int    `json:"updateCount,omitempty"`
}

const maxRunLogEntries = 100

// RunLog maintains a persistent JSONL log of cycle executions.
// Stored alongside the goal store in the same directory.
type RunLog struct {
	mu   sync.Mutex
	path string
	ring []RunLogEntry
}

// NewRunLog creates a run log in the same directory as the goal store.
func NewRunLog(goalStorePath string) *RunLog {
	dir := filepath.Dir(goalStorePath)
	logPath := filepath.Join(dir, "cycle-runs.jsonl")
	rl := &RunLog{path: logPath}
	rl.load()
	return rl
}

// Append adds a new entry and persists to disk.
func (rl *RunLog) Append(entry RunLogEntry) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.ring = append(rl.ring, entry)
	// Trim to max entries.
	if len(rl.ring) > maxRunLogEntries {
		rl.ring = rl.ring[len(rl.ring)-maxRunLogEntries:]
	}

	// Append one line to the JSONL file.
	dir := filepath.Dir(rl.path)
	os.MkdirAll(dir, 0o700)

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	f, err := os.OpenFile(rl.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(line)

	// Truncate file if it has grown beyond maxRunLogEntries.
	rl.mayTruncateFile()
}

// Recent returns the last n entries (newest last).
func (rl *RunLog) Recent(n int) []RunLogEntry {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if n <= 0 || len(rl.ring) == 0 {
		return nil
	}
	if n > len(rl.ring) {
		n = len(rl.ring)
	}
	out := make([]RunLogEntry, n)
	copy(out, rl.ring[len(rl.ring)-n:])
	return out
}

// load reads existing entries from the JSONL file on startup.
func (rl *RunLog) load() {
	data, err := os.ReadFile(rl.path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry RunLogEntry
		if json.Unmarshal([]byte(line), &entry) == nil {
			rl.ring = append(rl.ring, entry)
		}
	}
	// Keep only the last maxRunLogEntries.
	if len(rl.ring) > maxRunLogEntries {
		rl.ring = rl.ring[len(rl.ring)-maxRunLogEntries:]
	}
}

// mayTruncateFile rewrites the JSONL if the in-memory ring was trimmed,
// preventing the file from growing without bound.
func (rl *RunLog) mayTruncateFile() {
	// Count lines in file (cheap heuristic: check file size).
	info, err := os.Stat(rl.path)
	if err != nil {
		return
	}
	// Rough estimate: each entry ~150 bytes. Rewrite if file is 2x expected size.
	expectedSize := int64(len(rl.ring)) * 150
	if info.Size() <= expectedSize*2 {
		return
	}

	// Rewrite file with current ring contents.
	var buf []byte
	for _, entry := range rl.ring {
		line, _ := json.Marshal(entry)
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(rl.path, buf, 0o600); err != nil {
		// Non-critical: log file truncation failure doesn't affect operation.
		_ = err
	}
}
