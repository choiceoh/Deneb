package autoreply

import (
	"sync"
	"time"
)

// TypingController manages typing indicator lifecycle for a reply.
// It sends periodic typing signals to the channel until the reply completes.
//
// Enhanced to match the full TS TypingController interface:
// startTypingLoop, startTypingOnText, refreshTypingTtl, isActive,
// markRunComplete, markDispatchIdle, cleanup.
type TypingController struct {
	mu           sync.Mutex
	started      bool
	active       bool // true while keepalive loop is running
	sealed       bool
	runComplete  bool
	dispatchIdle bool
	done         chan struct{}
	onStart      func()
	onStop       func()
	onCleanup    func()
	intervalMs   int64
	ttlMs        int64 // auto-stop after TTL expires (default 30000ms)
	ttlDeadline  time.Time
	policy       TypingPolicy
	silentToken  string
}

// TypingControllerConfig configures the typing controller.
type TypingControllerConfig struct {
	OnStart      func() // called when typing begins (and on each keepalive tick)
	OnStop       func() // called when typing ends
	OnCleanup    func() // called on cleanup
	IntervalMs   int64  // keepalive interval (default 6000ms, matching TS)
	TtlMs        int64  // typing auto-stop TTL (default 30000ms)
	Policy       TypingPolicy
	SilentToken  string // silent reply token (default: NO_REPLY)
}

// NewTypingController creates a new typing controller.
func NewTypingController(cfg TypingControllerConfig) *TypingController {
	intervalMs := cfg.IntervalMs
	if intervalMs <= 0 {
		intervalMs = 6000 // TS default: typingIntervalSeconds = 6
	}
	ttlMs := cfg.TtlMs
	if ttlMs <= 0 {
		ttlMs = 30000 // TS default: typingTtlMs = 30000
	}
	silentToken := cfg.SilentToken
	if silentToken == "" {
		silentToken = SilentReplyToken
	}
	return &TypingController{
		onStart:     cfg.OnStart,
		onStop:      cfg.OnStop,
		onCleanup:   cfg.OnCleanup,
		intervalMs:  intervalMs,
		ttlMs:       ttlMs,
		policy:      cfg.Policy,
		silentToken: silentToken,
		done:        make(chan struct{}),
	}
}

// Start begins sending typing indicators. Safe to call multiple times.
// Alias for startTypingLoop for backward compatibility.
func (tc *TypingController) Start() {
	tc.StartTypingLoop()
}

// StartTypingLoop starts the typing keepalive loop. Refreshes the TTL timer.
// No-op if already started or sealed.
func (tc *TypingController) StartTypingLoop() {
	tc.mu.Lock()
	if tc.sealed || tc.runComplete {
		tc.mu.Unlock()
		return
	}
	if tc.started {
		// Already running — just refresh TTL.
		tc.ttlDeadline = time.Now().Add(time.Duration(tc.ttlMs) * time.Millisecond)
		tc.mu.Unlock()
		return
	}
	tc.started = true
	tc.active = true
	tc.ttlDeadline = time.Now().Add(time.Duration(tc.ttlMs) * time.Millisecond)
	tc.mu.Unlock()

	if tc.onStart != nil {
		tc.onStart()
	}

	// Keepalive loop with TTL expiry.
	go func() {
		ticker := time.NewTicker(time.Duration(tc.intervalMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-tc.done:
				return
			case <-ticker.C:
				tc.mu.Lock()
				if tc.sealed {
					tc.active = false
					tc.mu.Unlock()
					return
				}
				// Check TTL expiry.
				if time.Now().After(tc.ttlDeadline) {
					tc.active = false
					tc.mu.Unlock()
					return
				}
				tc.mu.Unlock()
				if tc.onStart != nil {
					tc.onStart()
				}
			}
		}
	}()
}

// StartTypingOnText starts typing only if the text is not a silent reply token.
// Checks both exact match and prefix match for streamed silent tokens.
func (tc *TypingController) StartTypingOnText(text string) {
	if tc == nil {
		return
	}
	trimmed := trimWhitespace(text)
	if trimmed == "" {
		return
	}
	// Skip silent reply tokens and their streamed prefixes.
	if IsSilentReplyText(trimmed, tc.silentToken) || IsSilentReplyPrefixText(trimmed, tc.silentToken) {
		return
	}
	tc.StartTypingLoop()
}

// RefreshTypingTtl extends the TTL deadline without restarting the loop.
func (tc *TypingController) RefreshTypingTtl() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if !tc.sealed {
		tc.ttlDeadline = time.Now().Add(time.Duration(tc.ttlMs) * time.Millisecond)
	}
}

// IsActive returns true if typing indicators are currently being sent.
func (tc *TypingController) IsActive() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.active && !tc.sealed
}

// MarkRunComplete signals that the agent run has finished.
// After a grace period, seals the controller.
func (tc *TypingController) MarkRunComplete() {
	tc.mu.Lock()
	if tc.runComplete || tc.sealed {
		tc.mu.Unlock()
		return
	}
	tc.runComplete = true
	tc.mu.Unlock()

	// Grace period: keep typing for up to 10 seconds after run completes,
	// matching the TS implementation.
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-tc.done:
		case <-timer.C:
			tc.Seal()
		}
	}()
}

// MarkDispatchIdle signals that dispatch has finished all pending deliveries.
func (tc *TypingController) MarkDispatchIdle() {
	tc.mu.Lock()
	tc.dispatchIdle = true
	shouldSeal := tc.runComplete
	tc.mu.Unlock()
	if shouldSeal {
		tc.Stop()
	}
}

// Cleanup stops typing and prevents re-entry. Calls onCleanup if set.
func (tc *TypingController) Cleanup() {
	tc.Stop()
	if tc.onCleanup != nil {
		tc.onCleanup()
	}
}

// Stop ends typing indicators and prevents further signals.
func (tc *TypingController) Stop() {
	tc.mu.Lock()
	if tc.sealed {
		tc.mu.Unlock()
		return
	}
	tc.sealed = true
	tc.active = false
	tc.mu.Unlock()

	select {
	case <-tc.done:
	default:
		close(tc.done)
	}

	if tc.onStop != nil {
		tc.onStop()
	}
}

// Seal prevents any further typing signals without calling onStop.
func (tc *TypingController) Seal() {
	tc.mu.Lock()
	tc.sealed = true
	tc.active = false
	tc.mu.Unlock()
	select {
	case <-tc.done:
	default:
		close(tc.done)
	}
}

// IsStarted returns whether typing was started.
func (tc *TypingController) IsStarted() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.started
}

// TypingSignaler wraps a TypingController for phase-aware signaling.
type TypingSignaler struct {
	controller *TypingController
	phase      string // "text", "reasoning", "tool"
}

// NewTypingSignaler creates a new typing signaler.
func NewTypingSignaler(controller *TypingController) *TypingSignaler {
	return &TypingSignaler{controller: controller, phase: "text"}
}

// SetPhase updates the current typing phase.
func (s *TypingSignaler) SetPhase(phase string) {
	s.phase = phase
}

// Signal sends a typing signal if appropriate for the current phase.
func (s *TypingSignaler) Signal() {
	if s.controller == nil {
		return
	}
	s.controller.Start()
}

// Stop stops the typing controller.
func (s *TypingSignaler) Stop() {
	if s.controller != nil {
		s.controller.Stop()
	}
}
