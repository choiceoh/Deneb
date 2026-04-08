package monitoring

import (
	"context"
	"testing"
	"time"

	"log/slog"
)

func TestChannelHealthMonitor_HealthSnapshot(t *testing.T) {
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		GetChannelStatus:      func() string { return "running" },
		GetChannelLastEventAt: func() int64 { return time.Now().UnixMilli() },
	}, DefaultChannelHealthConfig(), slog.Default())

	snap := m.HealthSnapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d, want 1 result", len(snap))
	}
	if snap[0].ChannelID != "telegram" {
		t.Errorf("got %q, want channelId telegram", snap[0].ChannelID)
	}
	if !snap[0].Healthy {
		t.Error("telegram should be healthy")
	}
}

func TestChannelHealthMonitor_HealthSnapshot_Stopped(t *testing.T) {
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		GetChannelStatus: func() string { return "stopped" },
	}, DefaultChannelHealthConfig(), slog.Default())

	snap := m.HealthSnapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d, want 1 result", len(snap))
	}
	if snap[0].Healthy {
		t.Error("stopped channel should be unhealthy")
	}
}

func TestChannelHealthMonitor_StaleChannelRestart(t *testing.T) {
	restarted := false
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		GetChannelStatus: func() string { return "running" },
		GetChannelLastEventAt: func() int64 {
			return time.Now().Add(-1 * time.Hour).UnixMilli() // stale
		},
		GetChannelStartedAt: func() int64 {
			return time.Now().Add(-2 * time.Hour).UnixMilli()
		},
		RestartChannel: func() error {
			restarted = true
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

	if !restarted {
		t.Error("expected telegram to be restarted")
	}
}

func TestChannelHealthMonitor_CooldownPreventsRestart(t *testing.T) {
	restartCount := 0
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		GetChannelStatus: func() string { return "running" },
		GetChannelLastEventAt: func() int64 {
			return time.Now().Add(-1 * time.Hour).UnixMilli()
		},
		GetChannelStartedAt: func() int64 {
			return time.Now().Add(-2 * time.Hour).UnixMilli()
		},
		RestartChannel: func() error {
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
		t.Errorf("got %d, want 1 restart (cooldown)", restartCount)
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

func TestChannelHealthMonitor_RunContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	m := NewChannelHealthMonitor(ChannelHealthDeps{}, ChannelHealthConfig{
		CheckIntervalMs:       10,
		MonitorStartupGraceMs: 100000,
	}, slog.Default())

	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK - exited on context cancel.
	case <-time.After(2 * time.Second):
		t.Error("Run did not exit after context cancel")
	}
}
