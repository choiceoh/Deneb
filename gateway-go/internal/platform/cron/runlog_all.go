// runlog_all.go — Cross-job run log aggregation.
// Mirrors src/cron/run-log.ts readCronRunLogEntriesPageAll().
package cron

import (
	"os"
	"path/filepath"
	"sort"
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
		return RunLogPageResult{Entries: []RunLogEntry{}, Total: 0, Offset: opts.Offset, Limit: clampLimit(opts.Limit)}
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

	// Apply filters.
	allEntries = filterEntries(allEntries, opts)

	// Sort by timestamp descending (newest first) by default.
	if opts.SortDir == "asc" {
		sort.Slice(allEntries, func(i, j int) bool {
			return allEntries[i].Ts < allEntries[j].Ts
		})
	} else {
		sort.Slice(allEntries, func(i, j int) bool {
			return allEntries[i].Ts > allEntries[j].Ts
		})
	}

	total := len(allEntries)
	limit := clampLimit(opts.Limit)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return RunLogPageResult{
			Entries: []RunLogEntry{},
			Total:   total,
			Offset:  offset,
			Limit:   limit,
		}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := allEntries[offset:end]
	hasMore := end < total
	var nextOffset *int
	if hasMore {
		no := end
		nextOffset = &no
	}

	return RunLogPageResult{
		Entries:    page,
		Total:      total,
		Offset:     offset,
		Limit:      limit,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}
}

func filterEntries(entries []RunLogEntry, opts RunLogReadOpts) []RunLogEntry {
	if opts.Status != "" && opts.Status != "all" {
		var filtered []RunLogEntry
		for _, e := range entries {
			if e.Status == opts.Status {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if opts.DeliveryStatus != "" {
		var filtered []RunLogEntry
		for _, e := range entries {
			if e.DeliveryStatus == opts.DeliveryStatus {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if opts.Query != "" {
		query := strings.ToLower(opts.Query)
		var filtered []RunLogEntry
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Summary), query) ||
				strings.Contains(strings.ToLower(e.Error), query) ||
				strings.Contains(strings.ToLower(e.JobID), query) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	return entries
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 5000 {
		return 200
	}
	return limit
}
