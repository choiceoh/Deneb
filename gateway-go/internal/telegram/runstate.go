package telegram

import (
	"context"
	"sync"
)

// RunState manages the run lifecycle of a long-running background goroutine.
// Embed it in your struct to get Stop() and IsRunning() for free and to
// replace the repeated Start preamble/epilogue with BeginRun/EndRun.
//
//	type Bot struct {
//	    channel.RunState            // provides IsRunning(), Stop(), BeginRun(), EndRun()
//	    stateMu sync.Mutex         // protects bot-specific fields
//	    ...
//	}
//
//	func (b *Bot) Start(ctx context.Context) error {
//	    runCtx, ok := b.BeginRun(ctx)
//	    if !ok {
//	        return nil              // already running
//	    }
//	    defer b.EndRun()
//	    return b.loop(runCtx)
//	}
type RunState struct {
	mu       sync.Mutex
	running  bool
	stopFunc context.CancelFunc
}

// IsRunning returns whether the background goroutine is currently running.
func (r *RunState) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Stop cancels the running goroutine's context. No-op if not running.
func (r *RunState) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopFunc != nil {
		r.stopFunc()
	}
}

// BeginRun transitions to the running state and returns a derived context.
// Returns (ctx, true) on success; (nil, false) if already running.
// The caller MUST call EndRun when the goroutine exits.
func (r *RunState) BeginRun(ctx context.Context) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil, false
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.running = true
	r.stopFunc = cancel
	return runCtx, true
}

// EndRun marks the goroutine as stopped and clears the cancel func.
func (r *RunState) EndRun() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
	r.stopFunc = nil
}
