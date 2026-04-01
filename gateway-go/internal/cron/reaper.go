package cron

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	defaultRetentionMs = 24 * 3600 * 1000 // 24 hours
	minSweepIntervalMs = 5 * 60 * 1000    // 5 minutes
)

// SessionReaper prunes completed cron run sessions after a retention period.
type SessionReaper struct {
	mu          sync.Mutex
	lastSweepAt int64
	retentionMs int64
	logger      *slog.Logger
}

// NewSessionReaper creates a new session reaper.
func NewSessionReaper(retentionMs int64, logger *slog.Logger) *SessionReaper {
	if retentionMs <= 0 {
		retentionMs = defaultRetentionMs
	}
	return &SessionReaper{
		retentionMs: retentionMs,
		logger:      logger,
	}
}

// ReaperResult reports the outcome of a sweep.
type ReaperResult struct {
	Swept  bool `json:"swept"`
	Pruned int  `json:"pruned"`
}

// IsCronRunSessionKey returns true if the key looks like a cron run session
// (including shadow sessions created for subagent cron jobs).
func IsCronRunSessionKey(key string) bool {
	if strings.HasPrefix(key, "cron:shadow:") {
		return true
	}
	return strings.Contains(key, ":cron:") && strings.Contains(key, ":run:")
}

// Sweep checks for and removes expired cron sessions.
// Self-throttles to avoid running more than once per minSweepIntervalMs.
// The cleanup function is called for each session key that should be removed.
func (r *SessionReaper) Sweep(nowMs int64, sessionKeys []string, cleanup func(key string)) ReaperResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	if nowMs-r.lastSweepAt < minSweepIntervalMs {
		return ReaperResult{Swept: false}
	}
	r.lastSweepAt = nowMs

	cutoff := nowMs - r.retentionMs
	pruned := 0

	for _, key := range sessionKeys {
		if !IsCronRunSessionKey(key) {
			continue
		}
		// Extract timestamp from key format: ...:cron:jobId:run:timestamp
		parts := strings.Split(key, ":")
		if len(parts) < 2 {
			continue
		}
		// The timestamp is typically the last segment.
		tsStr := parts[len(parts)-1]
		ts := parseTimestampFromKey(tsStr)
		if ts > 0 && ts < cutoff {
			cleanup(key)
			pruned++
		}
	}

	if pruned > 0 {
		r.logger.Info("cron session reaper swept", "pruned", pruned)
	}
	return ReaperResult{Swept: true, Pruned: pruned}
}

func parseTimestampFromKey(s string) int64 {
	// Try parsing as millisecond timestamp.
	var ts int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		ts = ts*10 + int64(c-'0')
	}
	// Sanity check: should be a reasonable timestamp (after 2020).
	if ts < 1577836800000 {
		return 0
	}
	return ts
}

// ResolveRetentionMs returns the configured retention or the default.
func ResolveRetentionMs(configuredMs int64) int64 {
	if configuredMs > 0 {
		return configuredMs
	}
	return defaultRetentionMs
}

// SweepPeriodically runs the reaper on a periodic interval.
func (r *SessionReaper) SweepPeriodically(ctx <-chan struct{}, intervalMs int64, getKeys func() []string, cleanup func(key string)) {
	if intervalMs <= 0 {
		intervalMs = minSweepIntervalMs
	}
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx:
			return
		case <-ticker.C:
			keys := getKeys()
			r.Sweep(time.Now().UnixMilli(), keys, cleanup)
		}
	}
}
