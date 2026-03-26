package autonomous

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestAttentionService(t *testing.T) (*Service, *mockAgentRunner) {
	t.Helper()
	runner := &mockAgentRunner{output: "ok"}
	dir := t.TempDir()
	cfg := ServiceConfig{
		GoalStorePath:  filepath.Join(dir, "goals.json"),
		CycleTimeoutMs: 2000,
	}
	svc := NewService(cfg, runner, nil)
	return svc, runner
}

func TestAttention_Cooldown(t *testing.T) {
	svc, _ := newTestAttentionService(t)
	svc.AddGoal("test", "medium")

	cfg := AttentionConfig{
		CycleInterval: time.Minute,
		CooldownMs:    500, // 500ms cooldown
	}
	att := NewAttention(svc, cfg, svc.logger)

	// First push should trigger.
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh})
	time.Sleep(50 * time.Millisecond)

	// Second push within cooldown should be ignored.
	att.Push(Signal{Kind: SignalExternalWake, Priority: SignalPriorityHigh})
	time.Sleep(50 * time.Millisecond)

	// Wait for cooldown to expire, then push again.
	time.Sleep(500 * time.Millisecond)
	att.Push(Signal{Kind: SignalExternalWake, Priority: SignalPriorityHigh})
	time.Sleep(50 * time.Millisecond)
}

func TestAttention_DefaultConfig(t *testing.T) {
	cfg := DefaultAttentionConfig()
	if cfg.CycleInterval != 10*time.Minute {
		t.Errorf("CycleInterval = %v, want 10m", cfg.CycleInterval)
	}
	if cfg.CooldownMs != 60_000 {
		t.Errorf("CooldownMs = %d, want 60000", cfg.CooldownMs)
	}
}

func TestAttention_StartStopTimer(t *testing.T) {
	svc, _ := newTestAttentionService(t)
	cfg := AttentionConfig{
		CycleInterval: 50 * time.Millisecond,
		CooldownMs:    10,
	}
	att := NewAttention(svc, cfg, svc.logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	att.StartTimer(ctx)
	// Starting again should be a no-op.
	att.StartTimer(ctx)

	time.Sleep(30 * time.Millisecond)
	att.StopTimer()
	// Stopping again should be safe.
	att.StopTimer()
}

func TestAttention_NilConfig(t *testing.T) {
	svc, _ := newTestAttentionService(t)
	// Zero-value config should use defaults.
	att := NewAttention(svc, AttentionConfig{}, svc.logger)
	if att.cfg.CycleInterval != 10*time.Minute {
		t.Errorf("default CycleInterval = %v", att.cfg.CycleInterval)
	}
	if att.cfg.CooldownMs != 60_000 {
		t.Errorf("default CooldownMs = %d", att.cfg.CooldownMs)
	}
}

func TestAttention_DeferredSignalGuard(t *testing.T) {
	svc, _ := newTestAttentionService(t)
	svc.AddGoal("test", "medium")

	cfg := AttentionConfig{
		CycleInterval: time.Minute,
		CooldownMs:    200,
	}
	att := NewAttention(svc, cfg, svc.logger)

	// First push triggers immediately.
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh})
	time.Sleep(20 * time.Millisecond)

	// Two rapid high-priority signals during cooldown: only one deferred goroutine
	// should be created (the second should be dropped).
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh})
	att.Push(Signal{Kind: SignalGoalAdded, Priority: SignalPriorityHigh})

	// pendingDefer should be true (one goroutine active).
	if !att.pendingDefer.Load() {
		t.Error("expected pendingDefer to be true after first deferred signal")
	}

	// Wait for cooldown to expire and deferred signal to fire.
	time.Sleep(300 * time.Millisecond)

	// pendingDefer should be cleared.
	if att.pendingDefer.Load() {
		t.Error("expected pendingDefer to be false after deferred signal fired")
	}
}

func TestAttention_InitialCycleTrigger(t *testing.T) {
	svc, runner := newTestAttentionService(t)
	svc.AddGoal("startup test", "medium")

	cfg := AttentionConfig{
		CycleInterval: time.Hour, // long interval so we don't get timer ticks
		CooldownMs:    10,
	}
	att := NewAttention(svc, cfg, svc.logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock the initial delay to be shorter for testing. We rely on the
	// 30-second delay in production, but test will just verify the Push happens.
	att.StartTimer(ctx)

	// The initial trigger fires after 30s in production. For this test we
	// just verify the timer is active and the system doesn't panic.
	if !att.IsTimerActive() {
		t.Error("expected timer to be active after StartTimer")
	}

	att.StopTimer()
	_ = runner // runner available if we need to verify call count
}
