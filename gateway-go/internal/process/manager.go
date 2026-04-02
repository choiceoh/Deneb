// Package process manages subprocess execution with approval workflow.
//
// This mirrors the process management in src/infra/process/ and the
// agent execution logic in src/agents/ from the TypeScript codebase.
// The Go gateway takes ownership of process lifecycle to avoid Node.js overhead.
package process

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
)

// gracefulStopDelay is how long to wait after SIGTERM before sending SIGKILL.
const gracefulStopDelay = 5 * time.Second

// RunStatus represents the current state of a managed process.
type RunStatus string

const (
	StatusPending  RunStatus = "pending"
	StatusApproved RunStatus = "approved"
	StatusRunning  RunStatus = "running"
	StatusDone     RunStatus = "done"
	StatusFailed   RunStatus = "failed"
	StatusKilled   RunStatus = "killed"
	StatusDenied   RunStatus = "denied"
)

// ExecRequest describes a command to execute.
type ExecRequest struct {
	ID         string            `json:"id"`
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	TimeoutMs  int64             `json:"timeoutMs,omitempty"`
	// RequiresApproval indicates whether the command needs explicit approval.
	RequiresApproval bool `json:"requiresApproval"`
}

// ExecResult is the outcome of a completed process.
type ExecResult struct {
	ID        string    `json:"id"`
	Status    RunStatus `json:"status"`
	ExitCode  int       `json:"exitCode"`
	Stdout    string    `json:"stdout,omitempty"`
	Stderr    string    `json:"stderr,omitempty"`
	StartedAt int64     `json:"startedAt"`
	EndedAt   int64     `json:"endedAt"`
	RuntimeMs int64     `json:"runtimeMs"`
	Error     string    `json:"error,omitempty"`
}

// TrackedProcess represents a running or completed process.
type TrackedProcess struct {
	Request ExecRequest `json:"request"`

	mu     sync.Mutex
	Status RunStatus   `json:"status"`           // guarded by mu
	Result *ExecResult `json:"result,omitempty"` // guarded by mu
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser // nil unless stdin pipe was created

	// Stream buffers for incremental output during execution.
	// Non-nil only while the process is running; after completion the
	// final output is stored in Result.Stdout/Stderr.
	stdoutBuf *StreamBuffer
	stderrBuf *StreamBuffer
}

// ApprovalCallback is called when a command requires approval.
// Return true to allow execution, false to deny.
type ApprovalCallback func(req ExecRequest) bool

// Manager manages subprocess lifecycle.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*TrackedProcess
	approver  ApprovalCallback
	logger    *slog.Logger
	maxStdout int // max bytes to capture per stream

	// cachedBaseEnv is the sanitized parent environment, computed once and
	// reused across sequential Execute calls to avoid repeated os.Environ()
	// + SanitizeEnv overhead.
	envOnce       sync.Once
	cachedBaseEnv []string

	stopPrune chan struct{} // closed to stop auto-prune goroutine
}

// NewManager creates a new process manager.
// It caches the sanitized parent environment and starts a background
// goroutine that prunes completed processes every 5 minutes.
func NewManager(logger *slog.Logger) *Manager {
	m := &Manager{
		processes: make(map[string]*TrackedProcess),
		logger:    logger,
		maxStdout: 1024 * 1024, // 1 MB default
		stopPrune: make(chan struct{}),
	}
	go m.autoPrune()
	return m
}

// Stop terminates the background prune goroutine. Safe to call multiple times.
func (m *Manager) Stop() {
	select {
	case <-m.stopPrune:
		// already stopped
	default:
		close(m.stopPrune)
	}
}

const (
	pruneInterval = 5 * time.Minute
	pruneMaxAge   = 10 * time.Minute
)

// autoPrune periodically removes completed processes older than pruneMaxAge.
func (m *Manager) autoPrune() {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopPrune:
			return
		case <-ticker.C:
			if n := m.Prune(pruneMaxAge); n > 0 {
				m.logger.Info("auto-pruned completed processes", "count", n)
			}
		}
	}
}

// baseEnv returns the cached sanitized parent environment.
func (m *Manager) baseEnv() []string {
	m.envOnce.Do(func() {
		m.cachedBaseEnv = SanitizeEnv(os.Environ(), m.logger)
	})
	return m.cachedBaseEnv
}

// InvalidateEnvCache forces re-computation of the cached base environment
// on the next Execute call. Use after modifying the process environment.
func (m *Manager) InvalidateEnvCache() {
	m.envOnce = sync.Once{}
	m.cachedBaseEnv = nil
}

// SetApprover sets the callback for approval-required commands.
func (m *Manager) SetApprover(cb ApprovalCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approver = cb
}

// Execute runs a command and returns the result. If RequiresApproval is set
// and no approver is configured or approval is denied, returns StatusDenied.
func (m *Manager) Execute(ctx context.Context, req ExecRequest) *ExecResult {
	if req.ID == "" {
		req.ID = shortid.New("proc")
	}

	tracked := &TrackedProcess{
		Request: req,
		Status:  StatusPending,
	}

	m.mu.Lock()
	m.processes[req.ID] = tracked
	m.mu.Unlock()

	// Approval gate.
	if req.RequiresApproval {
		m.mu.RLock()
		approver := m.approver
		m.mu.RUnlock()

		if approver == nil || !approver(req) {
			tracked.mu.Lock()
			tracked.Status = StatusDenied
			result := &ExecResult{
				ID:     req.ID,
				Status: StatusDenied,
				Error:  "execution denied",
			}
			tracked.Result = result
			tracked.mu.Unlock()
			return result
		}
		tracked.mu.Lock()
		tracked.Status = StatusApproved
		tracked.mu.Unlock()
	}

	// Build command.
	timeout := timeouts.ProcessExec
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	tracked.mu.Lock()
	tracked.cancel = cancel
	tracked.mu.Unlock()

	cmd := exec.CommandContext(execCtx, req.Command, req.Args...)
	// Run in a new process group so we can kill all children together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Graceful shutdown: send SIGTERM to the process group first, then SIGKILL
	// after gracefulStopDelay if the process hasn't exited.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = gracefulStopDelay
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	// Use cached sanitized base environment to avoid repeated os.Environ() +
	// SanitizeEnv overhead on sequential calls.
	base := m.baseEnv()
	cmd.Env = make([]string, len(base), len(base)+len(req.Env))
	copy(cmd.Env, base)
	// Overlay user-specified vars, also filtering blocked keys.
	for k, v := range req.Env {
		if isBlockedEnvKey(k) {
			m.logger.Info("exec sandbox: blocked user env var", "key", k)
			continue
		}
		if strings.ToUpper(k) == "NODE_OPTIONS" {
			v = sanitizeNodeOptions(v)
			if v == "" {
				continue
			}
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return m.failProcess(tracked, req.ID, time.Now().UnixMilli(), "stdout pipe: "+err.Error())
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return m.failProcess(tracked, req.ID, time.Now().UnixMilli(), "stderr pipe: "+err.Error())
	}

	// Create stdin pipe so background processes can receive input via WriteStdin.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return m.failProcess(tracked, req.ID, time.Now().UnixMilli(), "stdin pipe: "+err.Error())
	}

	tracked.mu.Lock()
	tracked.cmd = cmd
	tracked.stdin = stdinPipe
	tracked.Status = StatusRunning
	tracked.mu.Unlock()
	startedAt := time.Now().UnixMilli()

	m.logger.Info("process starting", "id", req.ID, "command", req.Command)

	if err := cmd.Start(); err != nil {
		cancel()
		return m.failProcess(tracked, req.ID, startedAt, err.Error())
	}

	// Drain both pipes concurrently into StreamBuffers. This allows
	// polling partial output while the process is still running.
	stdoutSB := NewStreamBuffer(m.maxStdout)
	stderrSB := NewStreamBuffer(m.maxStdout)
	tracked.mu.Lock()
	tracked.stdoutBuf = stdoutSB
	tracked.stderrBuf = stderrSB
	tracked.mu.Unlock()

	var drainWg sync.WaitGroup
	drainWg.Add(2)
	go func() {
		defer drainWg.Done()
		drainToBuffer(stdout, stdoutSB)
	}()
	go func() {
		defer drainWg.Done()
		drainToBuffer(stderr, stderrSB)
	}()

	// Wait for process exit BEFORE waiting for pipe drain. cmd.Wait()
	// triggers WaitDelay (SIGKILL after graceful timeout) and closes the
	// write end of pipes, unblocking drain goroutines. If we waited for
	// drain first, a grandchild process holding inherited pipe FDs would
	// block Read() indefinitely — preventing cmd.Wait() from ever running
	// and disabling the SIGKILL safety net.
	err = cmd.Wait()
	// Capture context error before cancel() overwrites it.
	ctxErr := execCtx.Err()
	cancel()

	// Bounded wait for pipe drain. Usually instant since Wait() confirmed
	// the process exited and closed the pipe write ends. But if grandchild
	// processes inherited pipe FDs and escaped the process group kill,
	// drain goroutines may still be blocked on Read(). Cap the wait to
	// avoid hanging the entire agent loop.
	drainDone := make(chan struct{})
	go func() { drainWg.Wait(); close(drainDone) }()
	select {
	case <-drainDone:
	case <-time.After(3 * time.Second):
		m.logger.Warn("process pipe drain timeout, proceeding with partial output", "id", req.ID)
		// Close the read ends to unblock stuck drain goroutines so they
		// don't leak. Safe to call — cmd.Wait() already finished.
		stdout.Close()
		stderr.Close()
	}

	endedAt := time.Now().UnixMilli()

	result := &ExecResult{
		ID:        req.ID,
		Stdout:    stdoutSB.Snapshot(),
		Stderr:    stderrSB.Snapshot(),
		StartedAt: startedAt,
		EndedAt:   endedAt,
		RuntimeMs: endedAt - startedAt,
	}

	if err != nil {
		switch ctxErr {
		case context.DeadlineExceeded:
			result.Status = StatusKilled
			result.Error = "timeout"
		case context.Canceled:
			// Parent context canceled (agent timeout) or explicit Kill() call.
			result.Status = StatusKilled
			result.Error = "canceled"
		default:
			result.Status = StatusFailed
			result.Error = err.Error()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	} else {
		result.Status = StatusDone
		result.ExitCode = 0
	}

	tracked.mu.Lock()
	tracked.Status = result.Status
	tracked.Result = result
	tracked.stdoutBuf = nil // release stream buffers after completion
	tracked.stderrBuf = nil
	tracked.mu.Unlock()
	m.logger.Info("process completed", "id", req.ID, "status", result.Status, "exitCode", result.ExitCode, "ms", result.RuntimeMs)
	return result
}

// ExecuteBackground launches the command in a goroutine and returns
// immediately with the process ID. Use Get(id) to poll for results.
func (m *Manager) ExecuteBackground(ctx context.Context, req ExecRequest) string {
	if req.ID == "" {
		req.ID = shortid.New("proc")
	}
	// Detach from the caller's context so the process outlives the RPC call,
	// but still respects server-level shutdown via the background context.
	bgCtx := context.WithoutCancel(ctx)
	go m.Execute(bgCtx, req)
	return req.ID
}

// WriteStdin writes data to a running process's stdin. Returns an error if
// the process is not found, not running, or stdin is unavailable.
func (m *Manager) WriteStdin(id string, data string) error {
	m.mu.RLock()
	tracked, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process not found: %s", id)
	}

	tracked.mu.Lock()
	defer tracked.mu.Unlock()

	if tracked.Status != StatusRunning {
		return fmt.Errorf("process not running: %s (status=%s)", id, tracked.Status)
	}
	if tracked.stdin == nil {
		return fmt.Errorf("stdin not available for process: %s", id)
	}
	_, err := io.WriteString(tracked.stdin, data)
	return err
}

// Kill terminates a running process.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	tracked, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process not found: %s", id)
	}

	tracked.mu.Lock()
	defer tracked.mu.Unlock()

	if tracked.Status != StatusRunning {
		return fmt.Errorf("process not running: %s (status=%s)", id, tracked.Status)
	}
	if tracked.cancel != nil {
		tracked.cancel()
	}
	tracked.Status = StatusKilled
	return nil
}

// ProcessSnapshot is a point-in-time copy of a TrackedProcess, safe to
// read/marshal without holding locks.
type ProcessSnapshot struct {
	Request       ExecRequest `json:"request"`
	Result        *ExecResult `json:"result,omitempty"`
	Status        RunStatus   `json:"status"`
	PartialStdout string      `json:"partialStdout,omitempty"`
	PartialStderr string      `json:"partialStderr,omitempty"`
}

func (tp *TrackedProcess) snapshot() ProcessSnapshot {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	snap := ProcessSnapshot{
		Request: tp.Request,
		Result:  tp.Result,
		Status:  tp.Status,
	}
	// Include partial output for running processes so poll can show progress.
	if tp.Status == StatusRunning {
		if tp.stdoutBuf != nil {
			snap.PartialStdout = tp.stdoutBuf.Snapshot()
		}
		if tp.stderrBuf != nil {
			snap.PartialStderr = tp.stderrBuf.Snapshot()
		}
	}
	return snap
}

// Get returns a snapshot of a tracked process by ID, or nil if not found.
func (m *Manager) Get(id string) *ProcessSnapshot {
	m.mu.RLock()
	tp := m.processes[id]
	m.mu.RUnlock()

	if tp == nil {
		return nil
	}
	snap := tp.snapshot()
	return &snap
}

// List returns snapshots of all tracked processes.
func (m *Manager) List() []ProcessSnapshot {
	m.mu.RLock()
	procs := make([]*TrackedProcess, 0, len(m.processes))
	for _, p := range m.processes {
		procs = append(procs, p)
	}
	m.mu.RUnlock()

	result := make([]ProcessSnapshot, 0, len(procs))
	for _, p := range procs {
		result = append(result, p.snapshot())
	}
	return result
}

// failProcess records a failed process and returns the result.
func (m *Manager) failProcess(tracked *TrackedProcess, id string, startedAt int64, errMsg string) *ExecResult {
	tracked.mu.Lock()
	tracked.Status = StatusFailed
	result := &ExecResult{
		ID:        id,
		Status:    StatusFailed,
		StartedAt: startedAt,
		EndedAt:   time.Now().UnixMilli(),
		Error:     errMsg,
	}
	tracked.Result = result
	tracked.mu.Unlock()
	return result
}

// drainBounded reads up to limit bytes into a pre-allocated buffer, then
// discards the rest to prevent the subprocess from blocking on a full pipe.
func drainBounded(r io.Reader, limit int) []byte {
	buf := make([]byte, limit)
	n, _ := io.ReadFull(r, buf)
	// Drain any remaining bytes so the writer doesn't block.
	io.Copy(io.Discard, r)
	return buf[:n]
}

// Prune removes completed/failed processes older than the given duration.
func (m *Manager) Prune(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge).UnixMilli()
	pruned := 0
	for id, p := range m.processes {
		p.mu.Lock()
		done := p.Result != nil && p.Result.EndedAt < cutoff
		p.mu.Unlock()
		if done {
			delete(m.processes, id)
			pruned++
		}
	}
	return pruned
}
