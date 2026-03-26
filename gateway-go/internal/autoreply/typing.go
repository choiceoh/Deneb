package autoreply

import (
	"sync"
	"time"
)

// TypingController manages typing indicator lifecycle for a reply.
// It sends periodic typing signals to the channel until the reply completes.
type TypingController struct {
	mu           sync.Mutex
	started      bool
	sealed       bool
	done         chan struct{}
	onStart      func()
	onStop       func()
	intervalMs   int64
	policy       TypingPolicy
	suppressText bool // suppress typing during text-only replies
}

// TypingControllerConfig configures the typing controller.
type TypingControllerConfig struct {
	OnStart      func() // called when typing begins
	OnStop       func() // called when typing ends
	IntervalMs   int64  // keepalive interval (default 4000ms)
	Policy       TypingPolicy
	Suppress     bool
}

// NewTypingController creates a new typing controller.
func NewTypingController(cfg TypingControllerConfig) *TypingController {
	intervalMs := cfg.IntervalMs
	if intervalMs <= 0 {
		intervalMs = 4000
	}
	return &TypingController{
		onStart:    cfg.OnStart,
		onStop:     cfg.OnStop,
		intervalMs: intervalMs,
		policy:     cfg.Policy,
		done:       make(chan struct{}),
	}
}

// Start begins sending typing indicators. Safe to call multiple times.
func (tc *TypingController) Start() {
	tc.mu.Lock()
	if tc.started || tc.sealed {
		tc.mu.Unlock()
		return
	}
	tc.started = true
	tc.mu.Unlock()

	if tc.onStart != nil {
		tc.onStart()
	}

	// Keepalive loop.
	go func() {
		ticker := time.NewTicker(time.Duration(tc.intervalMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-tc.done:
				return
			case <-ticker.C:
				tc.mu.Lock()
				sealed := tc.sealed
				tc.mu.Unlock()
				if sealed {
					return
				}
				if tc.onStart != nil {
					tc.onStart()
				}
			}
		}
	}()
}

// Stop ends typing indicators and prevents further signals.
func (tc *TypingController) Stop() {
	tc.mu.Lock()
	if tc.sealed {
		tc.mu.Unlock()
		return
	}
	tc.sealed = true
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
