// retention.go — age-based cleanup of dead session files.
//
// Every session gets its own JSONL file and nothing ever removed one: 5,018
// files had accumulated by 2026-08-25, 3,871 of them untouched for 90+ days.
// The per-file size prune (writer.go) bounds one busy session, not the pile.
// The pile is pure read amplification: every aggregation — the usage screen,
// the model tuner, regression watch — globs and parses EVERY file, and the
// widest consumer window anywhere is 31 days (miniapp.usage.stats clamps
// there; runtime-health reads 7d, token-economics is windowed too). A file
// whose last write is four months old can no longer contribute to anything.
package agentlog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// retentionMaxAge keeps ~6× the widest consumer window (31d) so a future
// consumer with a longer lookback has obvious headroom before this matters.
// Telemetry only — session transcripts live elsewhere and are not touched.
const retentionMaxAge = 180 * 24 * time.Hour

// PruneStaleFiles removes session log files whose last modification is older
// than retentionMaxAge. Returns how many files were removed. Best-effort: a
// file that cannot be removed is skipped, never fatal.
func (w *Writer) PruneStaleFiles(now time.Time) int {
	if w == nil || w.baseDir == "" {
		return 0
	}
	entries, err := os.ReadDir(w.baseDir)
	if err != nil {
		return 0
	}
	cutoff := now.Add(-retentionMaxAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if rerr := os.Remove(filepath.Join(w.baseDir, e.Name())); rerr != nil {
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("agentlog: pruned stale session files", "removed", removed, "olderThan", retentionMaxAge.String())
	}
	return removed
}
