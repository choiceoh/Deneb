// progressive.go adds progressive emission support to hook execution.
//
// Instead of blocking until all hooks complete and returning only final
// results, progressive emission sends HookProgress events through a channel
// as hooks execute. This enables real-time UI updates (e.g., showing which
// hook is running, its output, duration) instead of a silent wait.
//
// Inspired by Claude Code's async generator-based handleStopHooks pattern.
package hooks

import (
	"context"
	"sync"
	"time"
)

// HookProgress reports the status of a hook execution in real-time.
type HookProgress struct {
	HookID  string `json:"hookId"`
	Event   Event  `json:"event"`
	Command string `json:"command"`
	Phase   string `json:"phase"` // "started", "completed", "failed"

	// Set only when Phase is "completed" or "failed".
	ExitCode   int    `json:"exitCode,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
	HasOutput  bool   `json:"hasOutput,omitempty"` // true if stdout/stderr non-empty
}

// FireProgressive executes all hooks for the given event, sending progress
// events through the returned channel. The channel is closed when all hooks
// have completed. Final HookResults are also returned via the channel as
// "completed" or "failed" progress events.
//
// Blocking hooks run sequentially; non-blocking hooks run concurrently.
// Context cancellation is checked between hook executions.
func (r *Registry) FireProgressive(ctx context.Context, event Event, env map[string]string) <-chan HookProgress {
	ch := make(chan HookProgress, 16)

	go func() {
		defer close(ch)

		hooks := r.ListForEvent(event)
		if len(hooks) == 0 {
			return
		}

		var wg sync.WaitGroup

		for _, h := range hooks {
			// Check for cancellation between hooks.
			if ctx.Err() != nil {
				return
			}

			// Emit "started" progress.
			ch <- HookProgress{
				HookID:  h.ID,
				Event:   event,
				Command: h.Command,
				Phase:   "started",
			}

			if h.Blocking {
				// Run blocking hooks sequentially.
				result := r.executeHook(ctx, h, env)
				ch <- progressFromResult(result, h)
			} else {
				// Run non-blocking hooks concurrently.
				wg.Add(1)
				go func(hook Hook) {
					defer wg.Done()
					result := r.executeHook(ctx, hook, env)
					ch <- progressFromResult(result, hook)
				}(h)
			}
		}

		// Wait for all async hooks.
		wg.Wait()
	}()

	return ch
}

// progressFromResult converts a HookResult into a HookProgress event.
func progressFromResult(result HookResult, hook Hook) HookProgress {
	phase := "completed"
	errMsg := ""
	if result.ExitCode != 0 || result.Error != "" {
		phase = "failed"
		errMsg = result.Error
		if errMsg == "" {
			errMsg = result.Stderr
		}
	}

	return HookProgress{
		HookID:     hook.ID,
		Event:      hook.Event,
		Command:    hook.Command,
		Phase:      phase,
		ExitCode:   result.ExitCode,
		DurationMs: result.Duration,
		Error:      errMsg,
		HasOutput:  result.Stdout != "" || result.Stderr != "",
	}
}

// CollectProgress drains a progress channel and returns all events.
// Useful for tests and non-streaming consumers.
func CollectProgress(ch <-chan HookProgress) []HookProgress {
	var events []HookProgress
	for p := range ch {
		events = append(events, p)
	}
	return events
}

// TotalDuration returns the sum of all hook durations from progress events.
func TotalDuration(events []HookProgress) time.Duration {
	var total int64
	for _, e := range events {
		total += e.DurationMs
	}
	return time.Duration(total) * time.Millisecond
}
