package cron

import (
	"sort"
	"strings"
)

func buildRunLogPage(entries []RunLogEntry, opts RunLogReadOpts) RunLogPageResult {
	entries = filterRunLogEntries(entries, opts)
	sort.SliceStable(entries, func(i, j int) bool {
		if opts.SortDir == "asc" {
			return entries[i].Ts < entries[j].Ts
		}
		return entries[i].Ts > entries[j].Ts
	})

	limit := clampRunLogLimit(opts.Limit)
	offset := max(opts.Offset, 0)
	total := len(entries)
	if offset >= total {
		return RunLogPageResult{
			Entries: []RunLogEntry{},
			Total:   total,
			Offset:  offset,
			Limit:   limit,
		}
	}

	end := min(offset+limit, total)
	hasMore := end < total
	var nextOffset *int
	if hasMore {
		next := end
		nextOffset = &next
	}
	return RunLogPageResult{
		Entries:    entries[offset:end],
		Total:      total,
		Offset:     offset,
		Limit:      limit,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}
}

func filterRunLogEntries(entries []RunLogEntry, opts RunLogReadOpts) []RunLogEntry {
	query := strings.ToLower(opts.Query)
	filtered := make([]RunLogEntry, 0, len(entries))
	for _, entry := range entries {
		if opts.Status != "" && opts.Status != "all" && entry.Status != opts.Status {
			continue
		}
		if opts.DeliveryStatus != "" && entry.DeliveryStatus != opts.DeliveryStatus {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Summary), query) &&
			!strings.Contains(strings.ToLower(entry.Error), query) &&
			!strings.Contains(strings.ToLower(entry.JobID), query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func clampRunLogLimit(limit int) int {
	if limit <= 0 || limit > 5000 {
		return 200
	}
	return limit
}
