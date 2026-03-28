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
	Status RunStatus   `json:"status"`   // guarded by mu
	Result *ExecResult `json:"result,omitempty"` // guarded by mu
	cmd    *exec.Cmd
	cancel context.CancelFunc
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
}

// NewManager creates a new process manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		processes: make(map[string]*TrackedProcess),
		logger:    logger,
		maxStdout: 1024 * 1024, // 1 MB default
	}
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
	// Inherit parent environment with dangerous vars stripped.
	parentEnv := os.Environ()
	cmd.Env = SanitizeEnv(parentEnv, m.logger)
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

	tracked.mu.Lock()
	tracked.cmd = cmd
	tracked.Status = StatusRunning
	tracked.mu.Unlock()
	startedAt := time.Now().UnixMilli()

	m.logger.Info("process starting", "id", req.ID, "command", req.Command)

	if err := cmd.Start(); err != nil {
		cancel()
		return m.failProcess(tracked, req.ID, startedAt, err.Error())
	}

	// Capture output (bounded). We must fully drain both pipes even beyond the
	// capture limit, otherwise the subprocess blocks on a full pipe buffer and
	// cmd.Wait() hangs forever.
	stdoutBytes := drainBounded(stdout, m.maxStdout)
	stderrBytes := drainBounded(stderr, m.maxStdout)

	err = cmd.Wait()
	// Capture context error before cancel() overwrites it.
	ctxErr := execCtx.Err()
	cancel()
	endedAt := time.Now().UnixMilli()

	result := &ExecResult{
		ID:        req.ID,
		Stdout:    string(stdoutBytes),
		Stderr:    string(stderrBytes),
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
	tracked.mu.Unlock()
	m.logger.Info("process completed", "id", req.ID, "status", result.Status, "exitCode", result.ExitCode, "ms", result.RuntimeMs)
	return result
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
	Request ExecRequest `json:"request"`
	Result  *ExecResult `json:"result,omitempty"`
	Status  RunStatus   `json:"status"`
}

func (tp *TrackedProcess) snapshot() ProcessSnapshot {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return ProcessSnapshot{
		Request: tp.Request,
		Result:  tp.Result,
		Status:  tp.Status,
	}
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

// drainBounded reads up to limit bytes into memory, then discards the rest
// to prevent the subprocess from blocking on a full pipe.
func drainBounded(r io.Reader, limit int) []byte {
	kept, _ := io.ReadAll(io.LimitReader(r, int64(limit)))
	// Drain any remaining bytes so the writer doesn't block.
	io.Copy(io.Discard, r)
	return kept
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
