package agentlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// ReadOpts configures log reading.
type ReadOpts struct {
	SessionKey string
	RunID      string // optional: filter by run ID
	Type       string // optional: filter by entry type
	Limit      int    // max entries to return (default 50)
}

// ReadResult holds a page of log entries.
type ReadResult struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
}

// Read returns log entries for a session, filtered by opts.
// Entries are returned in reverse chronological order (newest first).
func (w *Writer) Read(opts ReadOpts) ReadResult {
	w.mu.Lock()
	defer w.mu.Unlock()

	path := w.logPath(opts.SessionKey)
	entries := readAllEntries(path)

	// Apply filters.
	if opts.RunID != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.RunID == opts.RunID {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if opts.Type != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.Type == opts.Type {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	total := len(entries)

	// Reverse for newest-first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// Apply limit.
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > len(entries) {
		limit = len(entries)
	}

	return ReadResult{
		Entries: entries[:limit],
		Total:   total,
	}
}

// readAllEntries reads all JSONL entries from a file.
func readAllEntries(path string) []LogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		for {
			var entry LogEntry
			if err := dec.Decode(&entry); err != nil {
				break // skip malformed tail (or EOF)
			}
			entries = append(entries, entry)
		}
	}
	return entries
}
