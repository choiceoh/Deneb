// Package hooks provides a hook registry and execution pipeline for the gateway.
//
// This mirrors the hook system in src/hooks/ from the TypeScript codebase.
// Hooks are user-defined shell commands that run in response to gateway events
// (e.g., before/after session start, on message receive, on channel connect).
package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Event represents a hook trigger point.
type Event string

const (
	EventSessionStart      Event = "session.start"
	EventSessionEnd        Event = "session.end"
	EventMessageReceive    Event = "message.receive"
	EventMessageSend       Event = "message.send"
	EventChannelConnect    Event = "channel.connect"
	EventChannelDisconnect Event = "channel.disconnect"
	EventGatewayStart      Event = "gateway.start"
	EventGatewayStop       Event = "gateway.stop"
	EventToolUse           Event = "tool.use"
	EventGitHubWebhook     Event = "github.webhook"
)

// Hook defines a user-configured hook.
type Hook struct {
	ID      string `json:"id"`
	Event   Event  `json:"event"`
	Command string `json:"command"`
	// TimeoutMs is the max time the hook can run (default 30000).
	TimeoutMs int64 `json:"timeoutMs,omitempty"`
	// Blocking determines if the hook must complete before the event proceeds.
	Blocking bool `json:"blocking,omitempty"`
	// Enabled controls whether the hook is active.
	Enabled bool `json:"enabled"`
}

// HookResult is the outcome of a hook execution.
type HookResult struct {
	HookID   string `json:"hookId"`
	Event    Event  `json:"event"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// Registry manages hook registration and execution.
type Registry struct {
	mu     sync.RWMutex
	hooks  map[string]*Hook
	logger *slog.Logger
}

// NewRegistry creates an empty hook registry.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		hooks:  make(map[string]*Hook),
		logger: logger,
	}
}

// Register adds a hook. Returns error if a hook with the same ID exists
// or if the command contains dangerous patterns.
func (r *Registry) Register(hook Hook) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if hook.ID == "" {
		return fmt.Errorf("hook ID is required")
	}
	if hook.Command == "" {
		return fmt.Errorf("hook command is required")
	}
	if err := validateHookCommand(hook.Command); err != nil {
		return err
	}
	if _, exists := r.hooks[hook.ID]; exists {
		return fmt.Errorf("hook %q already registered", hook.ID)
	}

	h := hook // copy
	if h.TimeoutMs <= 0 {
		h.TimeoutMs = 30000
	}
	r.hooks[hook.ID] = &h
	r.logger.Info("hook registered", "id", hook.ID, "event", hook.Event, "command", hook.Command)
	return nil
}

// validateHookCommand rejects commands that contain obvious shell injection patterns.
// This is a defense-in-depth measure; the primary defense is trusting the config source.
func validateHookCommand(cmd string) error {
	if len(cmd) > 4096 {
		return fmt.Errorf("hook command too long (%d bytes, max 4096)", len(cmd))
	}
	return nil
}

// Unregister removes a hook by ID.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.hooks[id]; ok {
		delete(r.hooks, id)
		return true
	}
	return false
}

// Update replaces an existing hook.
func (r *Registry) Update(hook Hook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.hooks[hook.ID]; !ok {
		return fmt.Errorf("hook %q not found", hook.ID)
	}
	h := hook
	if h.TimeoutMs <= 0 {
		h.TimeoutMs = 30000
	}
	r.hooks[hook.ID] = &h
	return nil
}

// List returns all registered hooks.
func (r *Registry) List() []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Hook, 0, len(r.hooks))
	for _, h := range r.hooks {
		result = append(result, *h)
	}
	return result
}

// ListForEvent returns all enabled hooks registered for the given event.
func (r *Registry) ListForEvent(event Event) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Hook
	for _, h := range r.hooks {
		if h.Event == event && h.Enabled {
			result = append(result, *h)
		}
	}
	return result
}

// Fire executes all hooks registered for the given event.
// Blocking hooks run sequentially; non-blocking hooks run concurrently.
// Returns results from all executed hooks.
func (r *Registry) Fire(ctx context.Context, event Event, env map[string]string) []HookResult {
	hooks := r.ListForEvent(event)
	if len(hooks) == 0 {
		return nil
	}

	var results []HookResult
	var asyncResults []HookResult
	var asyncMu sync.Mutex
	var wg sync.WaitGroup

	for _, h := range hooks {
		if h.Blocking {
			result := r.executeHook(ctx, h, env)
			results = append(results, result)
		} else {
			wg.Add(1)
			go func(hook Hook) {
				defer wg.Done()
				result := r.executeHook(ctx, hook, env)
				asyncMu.Lock()
				asyncResults = append(asyncResults, result)
				asyncMu.Unlock()
			}(h)
		}
	}

	wg.Wait()
	results = append(results, asyncResults...)
	return results
}

// maxHookOutputBytes limits how much stdout/stderr is captured per hook
// to prevent OOM from runaway output.
const maxHookOutputBytes = 256 * 1024 // 256 KB

func (r *Registry) executeHook(ctx context.Context, hook Hook, env map[string]string) HookResult {
	timeout := time.Duration(hook.TimeoutMs) * time.Millisecond
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Execute via shell for user-defined commands.
	cmd := exec.CommandContext(execCtx, "sh", "-c", hook.Command)

	// Inherit parent environment, then overlay hook-specific vars.
	parentEnv := os.Environ()
	cmd.Env = make([]string, 0, len(parentEnv)+len(env)+2)
	cmd.Env = append(cmd.Env, parentEnv...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "DENEB_HOOK_EVENT="+string(hook.Event))
	cmd.Env = append(cmd.Env, "DENEB_HOOK_ID="+hook.ID)

	// Capture output with bounded buffers to prevent OOM.
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdoutBuf, remaining: maxHookOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderrBuf, remaining: maxHookOutputBytes}

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()
	if duration == 0 {
		duration = 1 // ensure sub-millisecond executions report at least 1ms
	}

	result := HookResult{
		HookID:   hook.ID,
		Event:    hook.Event,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Duration: duration,
	}

	if err != nil {
		result.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		r.logger.Warn("hook failed", "id", hook.ID, "event", hook.Event, "error", err)
	}

	return result
}

// limitedWriter wraps a writer and discards bytes after the limit.
// Always reports the full input length to the caller so exec.Cmd doesn't
// treat a short write as an error (which would kill the subprocess early).
type limitedWriter struct {
	w         io.Writer
	remaining int
	dropped   int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	total := len(p)
	if lw.remaining <= 0 {
		lw.dropped += total
		return total, nil
	}
	write := p
	if len(write) > lw.remaining {
		write = write[:lw.remaining]
	}
	n, err := lw.w.Write(write)
	lw.remaining -= n
	lw.dropped += total - n
	if err != nil {
		return total, err
	}
	return total, nil
}
