// runlog_all.go — Cross-job run log aggregation.
// Mirrors src/cron/run-log.ts readCronRunLogEntriesPageAll().
package cron

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadPageAll reads run log entries from all jobs, aggregates, filters, and paginates.
// This enables the "all runs" view across all cron jobs.
func (rl *PersistentRunLog) ReadPageAll(opts RunLogReadOpts) RunLogPageResult {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	dir := rl.runsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return buildRunLogPage(nil, opts)
	}

	var allEntries []RunLogEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		logPath := filepath.Join(dir, entry.Name())
		fileEntries := readJSONLEntries(logPath)
		allEntries = append(allEntries, fileEntries...)
	}

	return buildRunLogPage(allEntries, opts)
}
