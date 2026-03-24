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
	"sync"
	"time"
)

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
	Result  *ExecResult `json:"result,omitempty"`
	Status  RunStatus   `json:"status"`
	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  context.CancelFunc
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
		req.ID = fmt.Sprintf("proc-%d", time.Now().UnixNano())
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
	timeout := 60 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	tracked.mu.Lock()
	tracked.cancel = cancel
	tracked.mu.Unlock()

	cmd := exec.CommandContext(execCtx, req.Command, req.Args...)
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	// Inherit parent environment, then overlay user-specified vars.
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	tracked.mu.Lock()
	tracked.cmd = cmd
	tracked.Status = StatusRunning
	tracked.mu.Unlock()
	startedAt := time.Now().UnixMilli()

	m.logger.Info("process starting", "id", req.ID, "command", req.Command)

	if err := cmd.Start(); err != nil {
		cancel()
		tracked.mu.Lock()
		tracked.Status = StatusFailed
		result := &ExecResult{
			ID:        req.ID,
			Status:    StatusFailed,
			StartedAt: startedAt,
			EndedAt:   time.Now().UnixMilli(),
			Error:     err.Error(),
		}
		tracked.Result = result
		tracked.mu.Unlock()
		return result
	}

	// Capture output (bounded).
	stdoutBytes, _ := io.ReadAll(io.LimitReader(stdout, int64(m.maxStdout)))
	stderrBytes, _ := io.ReadAll(io.LimitReader(stderr, int64(m.maxStdout)))

	err := cmd.Wait()
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
		if execCtx.Err() == context.DeadlineExceeded {
			result.Status = StatusKilled
			result.Error = "timeout"
		} else {
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

// Get returns a tracked process by ID.
func (m *Manager) Get(id string) *TrackedProcess {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processes[id]
}

// List returns all tracked processes.
func (m *Manager) List() []*TrackedProcess {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*TrackedProcess, 0, len(m.processes))
	for _, p := range m.processes {
		result = append(result, p)
	}
	return result
}

// Prune removes completed/failed processes older than the given duration.
func (m *Manager) Prune(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge).UnixMilli()
	pruned := 0
	for id, p := range m.processes {
		if p.Result != nil && p.Result.EndedAt < cutoff {
			delete(m.processes, id)
			pruned++
		}
	}
	return pruned
}
