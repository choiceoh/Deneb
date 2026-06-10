// memory_sweep.go — startup retention GC for memory stores.
//
// Two stores accumulated files forever: the Polaris raw message store (860
// files / 237MB observed in production, including a 107MB dead heartbeat
// file) and automated-session transcripts (1,400+ one-shot cron .jsonl
// files). Both sweeps run once at startup; the gateway restarts often, so a
// periodic timer would add nothing.
package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// defaultSweepRetentionDays is how long stale memory-store files are kept.
// Generous on purpose: cron transcripts have proven debugging value weeks
// later (a deleted cron job's prompt was once recovered from a 21-day-old
// transcript), so the window must comfortably exceed that.
const defaultSweepRetentionDays = 45

// memorySweepRetention returns the retention window for startup memory
// sweeps. DENEB_SWEEP_RETENTION_DAYS overrides the default; 0 or negative
// disables sweeping (used by dev live-test instances, which share the
// production state directories and must not GC them).
func memorySweepRetention() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_SWEEP_RETENTION_DAYS")); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			if d <= 0 {
				return 0
			}
			return time.Duration(d) * 24 * time.Hour
		}
	}
	return defaultSweepRetentionDays * 24 * time.Hour
}

// automatedTranscriptPrefixes are session-key prefixes whose transcripts are
// machine-generated and safe to age out: one-shot cron runs, ACP subagent
// forks, and ad-hoc ':'-prefixed harness/agent sessions. User-facing client:*
// and bounded system:* sessions are never swept —
// transcripts are primary records, so the sweep is allowlist-by-prefix, not
// mtime-only like the Polaris one.
var automatedTranscriptPrefixes = []string{"cron:", "acp:", ":"}

func isAutomatedTranscriptKey(key string) bool {
	for _, p := range automatedTranscriptPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// sweepAutomatedTranscripts removes automated-session transcript files whose
// last write is older than maxAge. Returns the number of files removed.
func sweepAutomatedTranscripts(dir string, maxAge time.Duration, logger *slog.Logger) int {
	if dir == "" || maxAge <= 0 {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if logger != nil && !os.IsNotExist(err) {
			logger.Warn("transcripts: sweep cannot read dir", "dir", dir, "error", err)
		}
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	var freed int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if !isAutomatedTranscriptKey(strings.TrimSuffix(name, ".jsonl")) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if rerr := os.Remove(filepath.Join(dir, name)); rerr != nil {
			if logger != nil {
				logger.Warn("transcripts: sweep remove failed", "file", name, "error", rerr)
			}
			continue
		}
		removed++
		freed += info.Size()
	}
	if removed > 0 && logger != nil {
		logger.Info("transcripts: swept automated session files",
			"removed", removed, "freedBytes", freed, "retention", maxAge.String())
	}
	return removed
}
