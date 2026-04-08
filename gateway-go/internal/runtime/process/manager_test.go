package process

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(testLogger())
	t.Cleanup(m.Stop)
	return m
}

func TestExecute_SimpleCommand(t *testing.T) {
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "test-1",
		Command: "echo",
		Args:    []string{"hello world"},
	})

	if result.Status != StatusDone {
		t.Errorf("got %s, want done", result.Status)
	}
	if result.ExitCode != 0 {
		t.Errorf("got %d, want exit code 0", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("expected non-empty stdout")
	}
	if result.RuntimeMs < 0 {
		t.Error("expected non-negative runtime")
	}
}

func TestExecute_FailedCommand(t *testing.T) {
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "fail-1",
		Command: "false",
	})

	if result.Status != StatusFailed {
		t.Errorf("got %s, want failed", result.Status)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestExecute_NonExistentCommand(t *testing.T) {
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "missing",
		Command: "/nonexistent/binary/xyz",
	})

	if result.Status != StatusFailed {
		t.Errorf("got %s, want failed", result.Status)
	}
}

func TestExecute_Timeout(t *testing.T) {
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:        "timeout-1",
		Command:   "sleep",
		Args:      []string{"10"},
		TimeoutMs: 100,
	})

	if result.Status != StatusKilled {
		t.Errorf("got %s, want killed", result.Status)
	}
}

func TestExecute_ApprovalDenied(t *testing.T) {
	m := newTestManager(t)
	m.SetApprover(func(_ ExecRequest) bool { return false })

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "denied-1",
		Command:          "echo",
		RequiresApproval: true,
	})

	if result.Status != StatusDenied {
		t.Errorf("got %s, want denied", result.Status)
	}
}

func TestExecute_ApprovalGranted(t *testing.T) {
	m := newTestManager(t)
	m.SetApprover(func(_ ExecRequest) bool { return true })

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "approved-1",
		Command:          "echo",
		Args:             []string{"approved"},
		RequiresApproval: true,
	})

	if result.Status != StatusDone {
		t.Errorf("got %s, want done", result.Status)
	}
}

func TestExecute_NoApprover(t *testing.T) {
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "no-approver",
		Command:          "echo",
		RequiresApproval: true,
	})

	if result.Status != StatusDenied {
		t.Errorf("got %s, want denied with no approver", result.Status)
	}
}

func TestGet_And_List(t *testing.T) {
	m := newTestManager(t)

	m.Execute(context.Background(), ExecRequest{ID: "a", Command: "echo", Args: []string{"a"}})
	m.Execute(context.Background(), ExecRequest{ID: "b", Command: "echo", Args: []string{"b"}})

	snap := m.Get("a")
	if snap == nil {
		t.Error("expected process 'a'")
	}
	if snap != nil && snap.Status != StatusDone {
		t.Errorf("got %s, want done", snap.Status)
	}
	if m.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent")
	}

	list := m.List()
	if len(list) != 2 {
		t.Errorf("got %d, want 2 processes", len(list))
	}
}

func TestKill(t *testing.T) {
	m := newTestManager(t)

	// Start a long-running process.
	go m.Execute(context.Background(), ExecRequest{
		ID:      "long",
		Command: "sleep",
		Args:    []string{"30"},
	})

	// Give it time to start.
	time.Sleep(100 * time.Millisecond)

	err := m.Kill("long")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKill_NotFound(t *testing.T) {
	m := newTestManager(t)
	err := m.Kill("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent process")
	}
}

func TestPrune(t *testing.T) {
	m := newTestManager(t)

	m.Execute(context.Background(), ExecRequest{ID: "old", Command: "echo"})
	time.Sleep(10 * time.Millisecond)

	pruned := m.Prune(1 * time.Millisecond)
	if pruned != 1 {
		t.Errorf("got %d, want 1 pruned", pruned)
	}

	if m.Get("old") != nil {
		t.Error("expected old process to be pruned")
	}
}

func TestAutoID(t *testing.T) {
	m := newTestManager(t)
	result := m.Execute(context.Background(), ExecRequest{Command: "echo"})
	if result.ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestExecute_ConcurrentPipeDrain(t *testing.T) {
	// Writes 128KB to stderr first, then stdout. With sequential drain this
	// would deadlock because stderr fills the pipe buffer before stdout is read.
	m := newTestManager(t)

	result := m.Execute(context.Background(), ExecRequest{
		ID:        "pipe-drain",
		Command:   "bash",
		Args:      []string{"-c", "dd if=/dev/zero bs=1024 count=128 >&2 2>/dev/null; echo done"},
		TimeoutMs: 5000,
	})

	if result.Status != StatusDone {
		t.Errorf("got %s (error: %s), want done", result.Status, result.Error)
	}
	if result.Stdout == "" {
		t.Error("expected non-empty stdout")
	}
}

func TestExecuteBackground(t *testing.T) {
	m := newTestManager(t)

	id := m.ExecuteBackground(context.Background(), ExecRequest{
		ID:      "bg-1",
		Command: "echo",
		Args:    []string{"background"},
	})

	if id != "bg-1" {
		t.Errorf("got %s, want id 'bg-1'", id)
	}

	// Poll until done (max 2 seconds).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := m.Get(id)
		if snap != nil && (snap.Status == StatusDone || snap.Status == StatusFailed) {
			if snap.Status != StatusDone {
				t.Errorf("got %s, want done", snap.Status)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("background process did not complete in time")
}

func TestParallelDrain_LargeOutput(t *testing.T) {
	// Verify that large concurrent stdout+stderr doesn't deadlock.
	m := newTestManager(t)
	// Generate 128KB on both stdout and stderr simultaneously.
	result := m.Execute(context.Background(), ExecRequest{
		ID:        "parallel-drain",
		Command:   "bash",
		Args:      []string{"-c", "dd if=/dev/zero bs=1024 count=128 2>/dev/null | tr '\\0' 'A'; dd if=/dev/zero bs=1024 count=128 | tr '\\0' 'B' >&2"},
		TimeoutMs: 10000,
	})
	if result.Status != StatusDone {
		t.Errorf("got %s (error: %s), want done", result.Status, result.Error)
	}
	if len(result.Stdout) < 100000 {
		t.Errorf("got %d bytes, want large stdout", len(result.Stdout))
	}
	if len(result.Stderr) < 100000 {
		t.Errorf("got %d bytes, want large stderr", len(result.Stderr))
	}
}

func TestEnvCache(t *testing.T) {
	m := newTestManager(t)

	// First call populates the cache.
	env1 := m.baseEnv()
	if len(env1) == 0 {
		t.Fatal("expected non-empty base env")
	}

	// Second call should return the same slice (cached).
	env2 := m.baseEnv()
	if len(env1) != len(env2) {
		t.Error("expected cached env to be identical")
	}

	// Invalidate and verify re-computation.
	m.InvalidateEnvCache()
	env3 := m.baseEnv()
	if len(env3) == 0 {
		t.Error("expected non-empty env after invalidation")
	}
}

func TestStop_Idempotent(t *testing.T) {
	m := NewManager(testLogger())
	m.Stop()
	m.Stop() // should not panic
}
