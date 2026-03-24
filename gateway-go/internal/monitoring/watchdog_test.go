package monitoring

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"
)

func TestWatchdog_SkipsGracePeriod(t *testing.T) {
	restartCalled := atomic.Bool{}
	w := NewWatchdog(WatchdogDeps{
		IsServerListening: func() bool { return false },
		OnRestartNeeded:   func(_ string) { restartCalled.Store(true) },
	}, WatchdogConfig{
		CheckIntervalMs:  50,
		StartupGraceMs:   5000, // 5 seconds grace
		StaleThresholdMs: 1000,
		MaxAutoRestarts:  3,
	}, slog.Default())

	// During grace period, check should not trigger restart.
	w.check()
	if restartCalled.Load() {
		t.Error("should not restart during grace period")
	}
}

func TestWatchdog_TriggersRestart_ServerNotListening(t *testing.T) {
	restartCalled := atomic.Bool{}
	restartReason := ""
	w := NewWatchdog(WatchdogDeps{
		IsServerListening: func() bool { return false },
		OnRestartNeeded: func(reason string) {
			restartCalled.Store(true)
			restartReason = reason
		},
	}, WatchdogConfig{
		CheckIntervalMs:  50,
		StartupGraceMs:   0, // no grace
		StaleThresholdMs: 30 * 60 * 1000,
		MaxAutoRestarts:  3,
	}, slog.Default())

	w.startedAt = time.Now().Add(-1 * time.Hour) // simulate past startup
	w.check()

	if !restartCalled.Load() {
		t.Error("should trigger restart when server not listening")
	}
	if restartReason != "server not listening" {
		t.Errorf("unexpected reason: %q", restartReason)
	}
}

func TestWatchdog_NoChannelsConnected(t *testing.T) {
	restartCalled := atomic.Bool{}
	w := NewWatchdog(WatchdogDeps{
		IsServerListening:        func() bool { return true },
		GetExpectedChannelCount:  func() int { return 3 },
		GetConnectedChannelCount: func() int { return 0 },
		OnRestartNeeded:          func(_ string) { restartCalled.Store(true) },
	}, WatchdogConfig{
		CheckIntervalMs:  50,
		StartupGraceMs:   0,
		StaleThresholdMs: 30 * 60 * 1000,
		MaxAutoRestarts:  3,
	}, slog.Default())

	w.startedAt = time.Now().Add(-1 * time.Hour)
	w.check()

	if !restartCalled.Load() {
		t.Error("should trigger restart when 0 channels connected")
	}
}

func TestWatchdog_RateLimitsRestarts(t *testing.T) {
	restartCount := 0
	w := NewWatchdog(WatchdogDeps{
		IsServerListening: func() bool { return false },
		OnRestartNeeded:   func(_ string) { restartCount++ },
	}, WatchdogConfig{
		CheckIntervalMs:  50,
		StartupGraceMs:   0,
		StaleThresholdMs: 30 * 60 * 1000,
		MaxAutoRestarts:  2,
	}, slog.Default())

	w.startedAt = time.Now().Add(-1 * time.Hour)
	w.check()
	w.check()
	w.check()
	w.check()

	if restartCount != 2 {
		t.Errorf("expected 2 restarts (rate limited), got %d", restartCount)
	}
}

func TestWatchdog_StaleActivity(t *testing.T) {
	restartCalled := atomic.Bool{}
	w := NewWatchdog(WatchdogDeps{
		IsServerListening: func() bool { return true },
		GetLastActivityAt: func() int64 {
			return time.Now().Add(-1 * time.Hour).UnixMilli()
		},
		OnRestartNeeded: func(_ string) { restartCalled.Store(true) },
	}, WatchdogConfig{
		CheckIntervalMs:  50,
		StartupGraceMs:   0,
		StaleThresholdMs: 10 * 1000, // 10 seconds
		MaxAutoRestarts:  3,
	}, slog.Default())

	w.startedAt = time.Now().Add(-1 * time.Hour)
	w.check()

	if !restartCalled.Load() {
		t.Error("should trigger restart for stale activity")
	}
}

func TestChannelHealthMonitor_HealthSnapshot(t *testing.T) {
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		ListChannelIDs:   func() []string { return []string{"discord", "telegram"} },
		GetChannelStatus: func(id string) string {
			if id == "discord" { return "running" }
			return "stopped"
		},
		GetChannelLastEventAt: func(_ string) int64 { return time.Now().UnixMilli() },
	}, DefaultChannelHealthConfig(), slog.Default())

	snap := m.HealthSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(snap))
	}

	for _, ch := range snap {
		if ch.ChannelID == "discord" && !ch.Healthy {
			t.Error("discord should be healthy")
		}
		if ch.ChannelID == "telegram" && ch.Healthy {
			t.Error("telegram should be unhealthy (stopped)")
		}
	}
}

func TestChannelHealthMonitor_StaleChannelRestart(t *testing.T) {
	restarted := ""
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		ListChannelIDs:   func() []string { return []string{"slack"} },
		GetChannelStatus: func(_ string) string { return "running" },
		GetChannelLastEventAt: func(_ string) int64 {
			return time.Now().Add(-1 * time.Hour).UnixMilli() // stale
		},
		GetChannelStartedAt: func(_ string) int64 {
			return time.Now().Add(-2 * time.Hour).UnixMilli()
		},
		RestartChannel: func(id string) error {
			restarted = id
			return nil
		},
	}, ChannelHealthConfig{
		CheckIntervalMs:       1000,
		MonitorStartupGraceMs: 0,
		ChannelConnectGraceMs: 0,
		StaleEventThresholdMs: 1000, // 1 second
		CooldownCycles:        2,
		MaxRestartsPerHour:    10,
	}, slog.Default())

	m.startedAt = time.Now().Add(-1 * time.Hour)
	m.check()

	if restarted != "slack" {
		t.Errorf("expected slack to be restarted, got %q", restarted)
	}
}

func TestChannelHealthMonitor_CooldownPreventsRestart(t *testing.T) {
	restartCount := 0
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		ListChannelIDs:   func() []string { return []string{"slack"} },
		GetChannelStatus: func(_ string) string { return "running" },
		GetChannelLastEventAt: func(_ string) int64 {
			return time.Now().Add(-1 * time.Hour).UnixMilli()
		},
		GetChannelStartedAt: func(_ string) int64 {
			return time.Now().Add(-2 * time.Hour).UnixMilli()
		},
		RestartChannel: func(_ string) error {
			restartCount++
			return nil
		},
	}, ChannelHealthConfig{
		CheckIntervalMs:       1000,
		MonitorStartupGraceMs: 0,
		ChannelConnectGraceMs: 0,
		StaleEventThresholdMs: 1000,
		CooldownCycles:        5, // 5 cycles cooldown
		MaxRestartsPerHour:    10,
	}, slog.Default())

	m.startedAt = time.Now().Add(-1 * time.Hour)
	m.check() // should restart
	m.check() // should be in cooldown
	m.check() // still cooldown

	if restartCount != 1 {
		t.Errorf("expected 1 restart (cooldown), got %d", restartCount)
	}
}

func TestActivityTracker(t *testing.T) {
	tracker := NewActivityTracker()
	initial := tracker.LastActivityAt()
	if initial <= 0 {
		t.Error("expected initial activity timestamp")
	}

	time.Sleep(2 * time.Millisecond)
	tracker.Touch()
	after := tracker.LastActivityAt()
	if after <= initial {
		t.Error("expected updated timestamp after Touch")
	}
}

func TestWatchdog_RunContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	w := NewWatchdog(WatchdogDeps{
		IsServerListening: func() bool { return true },
	}, WatchdogConfig{
		CheckIntervalMs:  10,
		StartupGraceMs:   100000,
		StaleThresholdMs: 100000,
		MaxAutoRestarts:  3,
	}, slog.Default())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK - exited on context cancel.
	case <-time.After(2 * time.Second):
		t.Error("Run did not exit after context cancel")
	}
}
