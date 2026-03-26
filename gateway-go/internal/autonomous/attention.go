package autonomous

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Signal kinds for attention triggers.
const (
	SignalGoalAdded    = "goal_added"
	SignalTimer        = "timer"
	SignalExternalWake = "external_wake"
)

// Signal priority levels.
const (
	SignalPriorityHigh   = "high"
	SignalPriorityNormal = "normal"
)

// Signal represents an attention trigger event.
type Signal struct {
	Kind     string // SignalGoalAdded, SignalTimer, SignalExternalWake
	Priority string // SignalPriorityHigh, SignalPriorityNormal
	Context  string // optional metadata
}

// AttentionConfig configures the attention system.
type AttentionConfig struct {
	CycleInterval time.Duration // periodic timer interval (default 10min)
	CooldownMs    int64         // min ms between triggered cycles (default 60s)
}

// DefaultAttentionConfig returns sensible defaults.
func DefaultAttentionConfig() AttentionConfig {
	return AttentionConfig{
		CycleInterval: 10 * time.Minute,
		CooldownMs:    60_000,
	}
}

// Attention accumulates signals and triggers autonomous cycles when thresholds are met.
type Attention struct {
	mu          sync.Mutex
	svc         *Service
	cfg         AttentionConfig
	lastTrigger int64
	timerCancel context.CancelFunc
	logger      *slog.Logger
}

// NewAttention creates an attention tracker for the given service.
func NewAttention(svc *Service, cfg AttentionConfig, logger *slog.Logger) *Attention {
	if cfg.CycleInterval <= 0 {
		cfg.CycleInterval = 10 * time.Minute
	}
	if cfg.CooldownMs <= 0 {
		cfg.CooldownMs = 60_000
	}
	return &Attention{
		svc:    svc,
		cfg:    cfg,
		logger: logger.With("component", "attention"),
	}
}

// Push receives a signal and may trigger a cycle based on priority and cooldown.
func (a *Attention) Push(signal Signal) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UnixMilli()

	// Cooldown check: don't trigger too frequently.
	if now-a.lastTrigger < a.cfg.CooldownMs {
		a.logger.Debug("attention signal within cooldown, ignoring",
			"kind", signal.Kind, "cooldownRemainMs", a.cfg.CooldownMs-(now-a.lastTrigger))
		return
	}

	// High-priority signals trigger immediately; normal signals also trigger
	// (the timer tick is "normal" priority).
	a.logger.Info("attention signal received, triggering cycle",
		"kind", signal.Kind, "priority", signal.Priority)
	a.lastTrigger = now

	// Run cycle in a goroutine to avoid blocking the caller.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := a.svc.RunCycle(ctx); err != nil {
			a.logger.Warn("attention-triggered cycle failed", "error", err)
		}
	}()
}

// StartTimer starts the periodic timer that sends normal-priority signals.
func (a *Attention) StartTimer(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.timerCancel != nil {
		return // already running
	}

	timerCtx, cancel := context.WithCancel(ctx)
	a.timerCancel = cancel

	go a.timerLoop(timerCtx)
	a.logger.Info("attention timer started", "interval", a.cfg.CycleInterval)
}

// StopTimer stops the periodic timer. Safe to call from any goroutine.
func (a *Attention) StopTimer() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopTimerLocked()
}

// stopTimerLocked cancels the timer without acquiring a.mu.
// Use this from code paths that already hold the lock (e.g. timerLoop).
func (a *Attention) stopTimerLocked() {
	if a.timerCancel != nil {
		a.timerCancel()
		a.timerCancel = nil
		a.logger.Info("attention timer stopped")
	}
}

func (a *Attention) timerLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.CycleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Skip if disabled.
			if !a.svc.Enabled() {
				continue
			}

			consErr := a.svc.ConsecutiveErrors()

			// Auto-stop after 10 consecutive failures.
			if consErr >= 10 {
				a.logger.Warn("10 consecutive failures, stopping attention timer")
				a.mu.Lock()
				a.stopTimerLocked()
				a.mu.Unlock()
				return
			}

			// Exponential backoff: skip tick if not enough time has elapsed.
			if consErr > 0 {
				exp := consErr
				if exp > 6 {
					exp = 6
				}
				backoffInterval := a.cfg.CycleInterval * time.Duration(int64(1)<<exp)
				a.mu.Lock()
				elapsed := time.Since(time.UnixMilli(a.lastTrigger))
				a.mu.Unlock()
				if elapsed < backoffInterval {
					a.logger.Debug("timer tick skipped due to backoff",
						"consecutiveErrors", consErr,
						"backoffInterval", backoffInterval,
						"elapsed", elapsed)
					continue
				}
			}

			a.Push(Signal{
				Kind:     SignalTimer,
				Priority: SignalPriorityNormal,
			})
		}
	}
}
