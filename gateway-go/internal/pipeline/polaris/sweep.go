// sweep.go — startup retention GC for the Polaris file store.
//
// The store never deleted anything on its own: every session (heartbeat,
// one-shot cron runs, dev live-test injections, retired channels) left its
// raw message JSONL behind forever. In production this accumulated 860 files
// / 237MB, with a single dead heartbeat file at 107MB. Sweeping by mtime at
// startup bounds the store without touching anything recently active.
//
// Only raw message files are swept. Summary files are kept on purpose: they
// are the condensed long-term memory of a session (~KB each), feed
// RecentSummariesAcrossSessions, and cost nothing to retain.
package polaris

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SweepExpired removes per-session raw message files whose last write is
// older than maxAge. Sessions currently loaded in memory are skipped (they
// are in active use this process). Returns the number of files removed.
// maxAge <= 0 disables sweeping entirely.
func (s *Store) SweepExpired(maxAge time.Duration, logger *slog.Logger) int {
	if maxAge <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	msgDir := filepath.Join(s.dir, "messages")
	entries, err := os.ReadDir(msgDir)
	if err != nil {
		if logger != nil && !os.IsNotExist(err) {
			logger.Warn("polaris: sweep cannot read messages dir", "dir", msgDir, "error", err)
		}
		return 0
	}

	removed := 0
	var freed int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".jsonl")
		s.mu.Lock()
		_, loaded := s.sessions[key]
		s.mu.Unlock()
		if loaded {
			continue
		}
		if rerr := os.Remove(filepath.Join(msgDir, e.Name())); rerr != nil {
			if logger != nil {
				logger.Warn("polaris: sweep remove failed", "file", e.Name(), "error", rerr)
			}
			continue
		}
		removed++
		freed += info.Size()
	}

	if removed > 0 && logger != nil {
		logger.Info("polaris: swept expired session message files",
			"removed", removed, "freedBytes", freed, "retention", maxAge.String())
	}
	return removed
}
