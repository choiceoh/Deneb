// Package monitoring implements the channel health monitor and activity trackers.
//
// The channel health monitor detects half-dead channels (connected but no
// events) and restarts them individually without killing the entire gateway.
package monitoring

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// --- Channel Health Monitor ---

// ChannelHealthDeps provides per-channel state queries.
type ChannelHealthDeps struct {
	// ListChannelIDs returns IDs of all configured channels.
	ListChannelIDs func() []string
	// GetChannelStatus returns the status of a channel ("running", "stopped", "error").
	GetChannelStatus func(id string) string
	// GetChannelLastEventAt returns the unix ms timestamp of the last event for a channel.
	GetChannelLastEventAt func(id string) int64
	// GetChannelStartedAt returns when the channel was started (unix ms).
	GetChannelStartedAt func(id string) int64
	// RestartChannel restarts a specific channel.
	RestartChannel func(id string) error
}

// ChannelHealthConfig tunes the channel health monitor.
type ChannelHealthConfig struct {
	CheckIntervalMs       int64 // default: 300000 (5 min)
	MonitorStartupGraceMs int64 // default: 60000 (1 min)
	ChannelConnectGraceMs int64 // default: 120000 (2 min)
	StaleEventThresholdMs int64 // default: 900000 (15 min)
	CooldownCycles        int   // default: 2 (10 min before retry)
	MaxRestartsPerHour    int   // default: 10
}

// DefaultChannelHealthConfig returns sensible defaults.
func DefaultChannelHealthConfig() ChannelHealthConfig {
	return ChannelHealthConfig{
		CheckIntervalMs:       5 * 60 * 1000,
		MonitorStartupGraceMs: 60 * 1000,
		ChannelConnectGraceMs: 2 * 60 * 1000,
		StaleEventThresholdMs: 15 * 60 * 1000,
		CooldownCycles:        2,
		MaxRestartsPerHour:    10,
	}
}

// ChannelHealthMonitor detects and restarts half-dead channels.
type ChannelHealthMonitor struct {
	deps   ChannelHealthDeps
	cfg    ChannelHealthConfig
	logger *slog.Logger

	mu             sync.Mutex
	startedAt      time.Time
	cooldowns      map[string]int // channelID -> remaining cooldown cycles
	restartHistory []time.Time
}

// NewChannelHealthMonitor creates a new channel health monitor.
func NewChannelHealthMonitor(deps ChannelHealthDeps, cfg ChannelHealthConfig, logger *slog.Logger) *ChannelHealthMonitor {
	if cfg.CheckIntervalMs == 0 {
		cfg = DefaultChannelHealthConfig()
	}
	return &ChannelHealthMonitor{
		deps:      deps,
		cfg:       cfg,
		logger:    logger,
		startedAt: time.Now(),
		cooldowns: make(map[string]int),
	}
}

// Run starts the channel health monitor loop. Blocks until context is canceled.
func (m *ChannelHealthMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.cfg.CheckIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check()
		}
	}
}

func (m *ChannelHealthMonitor) check() {
	now := time.Now()

	// Skip during startup grace period.
	if now.Sub(m.startedAt) < time.Duration(m.cfg.MonitorStartupGraceMs)*time.Millisecond {
		return
	}

	if m.deps.ListChannelIDs == nil {
		return
	}

	ids := m.deps.ListChannelIDs()
	for _, id := range ids {
		m.checkChannel(id, now)
	}

	// Decay cooldowns each cycle.
	m.mu.Lock()
	for id, remaining := range m.cooldowns {
		if remaining <= 1 {
			delete(m.cooldowns, id)
		} else {
			m.cooldowns[id] = remaining - 1
		}
	}
	m.mu.Unlock()
}

// ChannelHealthResult describes the health evaluation of a single channel.
type ChannelHealthResult struct {
	ChannelID string `json:"channelId"`
	Healthy   bool   `json:"healthy"`
	Reason    string `json:"reason,omitempty"`
}

func (m *ChannelHealthMonitor) checkChannel(id string, now time.Time) {
	status := "unknown"
	if m.deps.GetChannelStatus != nil {
		status = m.deps.GetChannelStatus(id)
	}

	// Only check running channels.
	if status != "running" {
		return
	}

	// Channel connect grace period.
	if m.deps.GetChannelStartedAt != nil {
		startedAt := m.deps.GetChannelStartedAt(id)
		if startedAt > 0 {
			elapsed := now.UnixMilli() - startedAt
			if elapsed < m.cfg.ChannelConnectGraceMs {
				return
			}
		}
	}

	// Check for stale events.
	if m.deps.GetChannelLastEventAt == nil {
		return
	}
	lastEvent := m.deps.GetChannelLastEventAt(id)
	if lastEvent <= 0 {
		return // No events yet — within grace.
	}

	staleMs := now.UnixMilli() - lastEvent
	if staleMs <= m.cfg.StaleEventThresholdMs {
		return // Healthy.
	}

	// Channel is stale — attempt restart.
	m.mu.Lock()
	if cooldown, ok := m.cooldowns[id]; ok && cooldown > 0 {
		m.mu.Unlock()
		return // In cooldown.
	}

	// Rate limit check.
	cutoff := now.Add(-1 * time.Hour)
	filtered := m.restartHistory[:0]
	for _, t := range m.restartHistory {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	m.restartHistory = filtered

	if len(m.restartHistory) >= m.cfg.MaxRestartsPerHour {
		m.mu.Unlock()
		m.logger.Warn("channel health: restart suppressed (max restarts per hour)",
			"channel", id, "staleMs", staleMs)
		return
	}

	m.restartHistory = append(m.restartHistory, now)
	m.cooldowns[id] = m.cfg.CooldownCycles
	m.mu.Unlock()

	m.logger.Warn("channel health: restarting stale channel",
		"channel", id,
		"staleMinutes", staleMs/60000,
	)

	if m.deps.RestartChannel != nil {
		if err := m.deps.RestartChannel(id); err != nil {
			m.logger.Error("channel health: restart failed", "channel", id, "error", err)
		}
	}
}

// HealthSnapshot returns the current health status for all channels.
func (m *ChannelHealthMonitor) HealthSnapshot() []ChannelHealthResult {
	if m.deps.ListChannelIDs == nil {
		return nil
	}

	ids := m.deps.ListChannelIDs()
	results := make([]ChannelHealthResult, 0, len(ids))
	now := time.Now()

	for _, id := range ids {
		result := ChannelHealthResult{ChannelID: id, Healthy: true}

		status := "unknown"
		if m.deps.GetChannelStatus != nil {
			status = m.deps.GetChannelStatus(id)
		}
		if status != "running" {
			result.Healthy = false
			result.Reason = "not running (status: " + status + ")"
			results = append(results, result)
			continue
		}

		if m.deps.GetChannelLastEventAt != nil {
			lastEvent := m.deps.GetChannelLastEventAt(id)
			if lastEvent > 0 {
				staleMs := now.UnixMilli() - lastEvent
				if staleMs > m.cfg.StaleEventThresholdMs {
					result.Healthy = false
					result.Reason = "stale (" + itoa(int(staleMs/60000)) + " minutes since last event)"
				}
			}
		}

		results = append(results, result)
	}

	return results
}

// --- Activity Tracker ---

// ActivityTracker records the timestamp of the last gateway activity.
type ActivityTracker struct {
	lastActivityMs atomic.Int64
}

// NewActivityTracker creates a new activity tracker.
func NewActivityTracker() *ActivityTracker {
	t := &ActivityTracker{}
	t.Touch()
	return t
}

// Touch updates the last activity timestamp to now.
func (t *ActivityTracker) Touch() {
	t.lastActivityMs.Store(time.Now().UnixMilli())
}

// LastActivityAt returns the last activity timestamp in unix milliseconds.
func (t *ActivityTracker) LastActivityAt() int64 {
	return t.lastActivityMs.Load()
}

// --- Channel Event Tracker ---

// ChannelEventTracker records per-channel event timestamps for health monitoring.
type ChannelEventTracker struct {
	mu     sync.RWMutex
	events map[string]int64 // channelID -> last event unix ms
}

// NewChannelEventTracker creates a new per-channel event tracker.
func NewChannelEventTracker() *ChannelEventTracker {
	return &ChannelEventTracker{
		events: make(map[string]int64),
	}
}

// Touch records an event for a specific channel.
func (t *ChannelEventTracker) Touch(channelID string) {
	now := time.Now().UnixMilli()
	t.mu.Lock()
	t.events[channelID] = now
	t.mu.Unlock()
}

// LastEventAt returns the last event timestamp for a channel, or 0 if unknown.
func (t *ChannelEventTracker) LastEventAt(channelID string) int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.events[channelID]
}

// Remove clears tracking for a channel (e.g., on disconnect).
func (t *ChannelEventTracker) Remove(channelID string) {
	t.mu.Lock()
	delete(t.events, channelID)
	t.mu.Unlock()
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
