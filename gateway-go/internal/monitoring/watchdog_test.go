package monitoring

import (
	"context"
	"testing"
	"time"

	"log/slog"
)

func TestChannelHealthMonitor_HealthSnapshot(t *testing.T) {
	m := NewChannelHealthMonitor(ChannelHealthDeps{
		ListChannelIDs: func() []string { return []string{"discord", "telegram"} },
		GetChannelStatus: func(id string) string {
			if id == "discord" {
				return "running"
			}
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

func TestChannelHealthMonitor_RunContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	m := NewChannelHealthMonitor(ChannelHealthDeps{
		ListChannelIDs: func() []string { return nil },
	}, ChannelHealthConfig{
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
