// Package monitoring implements the gateway self-watchdog and channel health monitor.
//
// This mirrors src/gateway/monitoring/gateway-self-watchdog.ts and
// src/gateway/monitoring/channel-health-monitor.ts from the TypeScript codebase.
//
// Two-tier monitoring:
// 1. Self-watchdog: detects stuck gateway (server not listening, no channels connected)
// 2. Channel health monitor: detects half-dead channels (connected but no events)
package monitoring

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// --- Gateway Self-Watchdog ---

// WatchdogDeps provides the gateway state checks needed by the watchdog.
type WatchdogDeps struct {
	// IsServerListening returns true if the HTTP server is accepting connections.
	IsServerListening func() bool
	// GetExpectedChannelCount returns the number of configured channels.
	GetExpectedChannelCount func() int
	// GetConnectedChannelCount returns the number of currently connected channels.
	GetConnectedChannelCount func() int
	// OnRestartNeeded is called when the watchdog determines a restart is needed.
	OnRestartNeeded func(reason string)
}

// WatchdogConfig tunes the watchdog behavior.
type WatchdogConfig struct {
	CheckIntervalMs  int64 // default: 120000 (2 min)
	StartupGraceMs   int64 // default: 180000 (3 min)
	MaxAutoRestarts int // default: 3 per hour
}

// DefaultWatchdogConfig returns sensible defaults.
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		CheckIntervalMs: 2 * 60 * 1000,
		StartupGraceMs:  3 * 60 * 1000,
		MaxAutoRestarts: 3,
	}
}

// Watchdog monitors gateway health and triggers restarts when stuck.
type Watchdog struct {
	deps   WatchdogDeps
	cfg    WatchdogConfig
	logger *slog.Logger

	mu             sync.Mutex
	startedAt      time.Time
	restartHistory []time.Time // timestamps of recent restarts
}

// NewWatchdog creates a new gateway self-watchdog.
func NewWatchdog(deps WatchdogDeps, cfg WatchdogConfig, logger *slog.Logger) *Watchdog {
	if cfg.CheckIntervalMs == 0 {
		cfg = DefaultWatchdogConfig()
	}
	return &Watchdog{
		deps:      deps,
		cfg:       cfg,
		logger:    logger,
		startedAt: time.Now(),
	}
}

// Run starts the watchdog loop. Blocks until context is canceled.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(w.cfg.CheckIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *Watchdog) check() {
	now := time.Now()

	// Skip during startup grace period.
	if now.Sub(w.startedAt) < time.Duration(w.cfg.StartupGraceMs)*time.Millisecond {
		return
	}

	// Check 1: Server listening.
	if w.deps.IsServerListening != nil && !w.deps.IsServerListening() {
		w.triggerRestart("server not listening")
		return
	}

	// Check 2: Channels connected.
	if w.deps.GetExpectedChannelCount != nil && w.deps.GetConnectedChannelCount != nil {
		expected := w.deps.GetExpectedChannelCount()
		connected := w.deps.GetConnectedChannelCount()
		if expected > 0 && connected == 0 {
			w.triggerRestart("no channels connected (0/" + itoa(expected) + " expected)")
			return
		}
	}

	// Note: stale-activity check removed. For a single-user deployment,
	// inactivity is normal and should not trigger a gateway restart.
	// The server-listening and channels-connected checks above are
	// sufficient to detect a truly stuck gateway.
}

func (w *Watchdog) triggerRestart(reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Clean up restart history (rolling 1-hour window).
	cutoff := time.Now().Add(-1 * time.Hour)
	filtered := w.restartHistory[:0]
	for _, t := range w.restartHistory {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	w.restartHistory = filtered

	// Rate limit restarts.
	if len(w.restartHistory) >= w.cfg.MaxAutoRestarts {
		w.logger.Warn("watchdog: restart suppressed (max restarts reached)",
			"reason", reason,
			"restartsInLastHour", len(w.restartHistory),
			"max", w.cfg.MaxAutoRestarts,
		)
		return
	}

	w.restartHistory = append(w.restartHistory, time.Now())
	w.logger.Warn("watchdog: triggering restart", "reason", reason)

	if w.deps.OnRestartNeeded != nil {
		w.deps.OnRestartNeeded(reason)
	}
}

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
