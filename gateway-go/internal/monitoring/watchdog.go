// Package monitoring implements the Telegram channel health monitor and activity trackers.
//
// The health monitor detects a half-dead Telegram connection (connected but no
// events) and restarts it without killing the entire gateway.
package monitoring

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// --- Telegram Health Monitor ---

// ChannelHealthDeps provides Telegram channel state queries.
type ChannelHealthDeps struct {
	// GetChannelStatus returns "running", "stopped", or "error".
	GetChannelStatus func() string
	// GetChannelLastEventAt returns the unix ms timestamp of the last event.
	GetChannelLastEventAt func() int64
	// GetChannelStartedAt returns when the channel was started (unix ms).
	GetChannelStartedAt func() int64
	// RestartChannel restarts the Telegram channel.
	RestartChannel func() error
}

// ChannelHealthConfig tunes the health monitor.
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

// ChannelHealthMonitor detects and restarts a half-dead Telegram channel.
type ChannelHealthMonitor struct {
	deps   ChannelHealthDeps
	cfg    ChannelHealthConfig
	logger *slog.Logger

	mu             sync.Mutex
	startedAt      time.Time
	cooldown       int // remaining cooldown cycles
	restartHistory []time.Time
}

// NewChannelHealthMonitor creates a new health monitor.
func NewChannelHealthMonitor(deps ChannelHealthDeps, cfg ChannelHealthConfig, logger *slog.Logger) *ChannelHealthMonitor {
	if cfg.CheckIntervalMs == 0 {
		cfg = DefaultChannelHealthConfig()
	}
	return &ChannelHealthMonitor{
		deps:      deps,
		cfg:       cfg,
		logger:    logger,
		startedAt: time.Now(),
	}
}

// Run starts the health monitor loop. Blocks until context is canceled.
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

	m.checkChannel(now)

	// Decay cooldown each cycle.
	m.mu.Lock()
	if m.cooldown > 0 {
		m.cooldown--
	}
	m.mu.Unlock()
}

// ChannelHealthResult describes the health evaluation of the Telegram channel.
type ChannelHealthResult struct {
	ChannelID string `json:"channelId"`
	Healthy   bool   `json:"healthy"`
	Reason    string `json:"reason,omitempty"`
}

func (m *ChannelHealthMonitor) checkChannel(now time.Time) {
	status := "unknown"
	if m.deps.GetChannelStatus != nil {
		status = m.deps.GetChannelStatus()
	}

	// Only check when running.
	if status != "running" {
		return
	}

	// Channel connect grace period.
	if m.deps.GetChannelStartedAt != nil {
		startedAt := m.deps.GetChannelStartedAt()
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
	lastEvent := m.deps.GetChannelLastEventAt()
	if lastEvent <= 0 {
		return // No events yet — within grace.
	}

	staleMs := now.UnixMilli() - lastEvent
	if staleMs <= m.cfg.StaleEventThresholdMs {
		return // Healthy.
	}

	// Channel is stale — attempt restart.
	m.mu.Lock()
	if m.cooldown > 0 {
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
			"staleMs", staleMs)
		return
	}

	m.restartHistory = append(m.restartHistory, now)
	m.cooldown = m.cfg.CooldownCycles
	m.mu.Unlock()

	m.logger.Warn("channel health: restarting stale telegram",
		"staleMinutes", staleMs/60000,
	)

	if m.deps.RestartChannel != nil {
		if err := m.deps.RestartChannel(); err != nil {
			m.logger.Error("channel health: restart failed", "error", err)
		}
	}
}

// HealthSnapshot returns the current health status of the Telegram channel.
func (m *ChannelHealthMonitor) HealthSnapshot() []ChannelHealthResult {
	result := ChannelHealthResult{ChannelID: "telegram", Healthy: true}
	now := time.Now()

	status := "unknown"
	if m.deps.GetChannelStatus != nil {
		status = m.deps.GetChannelStatus()
	}
	if status != "running" {
		result.Healthy = false
		result.Reason = "not running (status: " + status + ")"
		return []ChannelHealthResult{result}
	}

	if m.deps.GetChannelLastEventAt != nil {
		lastEvent := m.deps.GetChannelLastEventAt()
		if lastEvent > 0 {
			staleMs := now.UnixMilli() - lastEvent
			if staleMs > m.cfg.StaleEventThresholdMs {
				result.Healthy = false
				result.Reason = "stale (" + itoa(int(staleMs/60000)) + " minutes since last event)"
			}
		}
	}

	return []ChannelHealthResult{result}
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

// ChannelEventTracker records the last event timestamp for health monitoring.
type ChannelEventTracker struct {
	lastEventMs atomic.Int64
}

// NewChannelEventTracker creates a new event tracker.
func NewChannelEventTracker() *ChannelEventTracker {
	return &ChannelEventTracker{}
}

// Touch records an event.
func (t *ChannelEventTracker) Touch() {
	t.lastEventMs.Store(time.Now().UnixMilli())
}

// LastEventAt returns the last event timestamp, or 0 if unknown.
func (t *ChannelEventTracker) LastEventAt() int64 {
	return t.lastEventMs.Load()
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
