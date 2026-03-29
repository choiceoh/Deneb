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

func TestExecute_SimpleCommand(t *testing.T) {
	m := NewManager(testLogger())

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "test-1",
		Command: "echo",
		Args:    []string{"hello world"},
	})

	if result.Status != StatusDone {
		t.Errorf("expected done, got %s", result.Status)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("expected non-empty stdout")
	}
	if result.RuntimeMs < 0 {
		t.Error("expected non-negative runtime")
	}
}

func TestExecute_FailedCommand(t *testing.T) {
	m := NewManager(testLogger())

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "fail-1",
		Command: "false",
	})

	if result.Status != StatusFailed {
		t.Errorf("expected failed, got %s", result.Status)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestExecute_NonExistentCommand(t *testing.T) {
	m := NewManager(testLogger())

	result := m.Execute(context.Background(), ExecRequest{
		ID:      "missing",
		Command: "/nonexistent/binary/xyz",
	})

	if result.Status != StatusFailed {
		t.Errorf("expected failed, got %s", result.Status)
	}
}

func TestExecute_Timeout(t *testing.T) {
	m := NewManager(testLogger())

	result := m.Execute(context.Background(), ExecRequest{
		ID:        "timeout-1",
		Command:   "sleep",
		Args:      []string{"10"},
		TimeoutMs: 100,
	})

	if result.Status != StatusKilled {
		t.Errorf("expected killed, got %s", result.Status)
	}
}

func TestExecute_ApprovalDenied(t *testing.T) {
	m := NewManager(testLogger())
	m.SetApprover(func(_ ExecRequest) bool { return false })

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "denied-1",
		Command:          "echo",
		RequiresApproval: true,
	})

	if result.Status != StatusDenied {
		t.Errorf("expected denied, got %s", result.Status)
	}
}

func TestExecute_ApprovalGranted(t *testing.T) {
	m := NewManager(testLogger())
	m.SetApprover(func(_ ExecRequest) bool { return true })

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "approved-1",
		Command:          "echo",
		Args:             []string{"approved"},
		RequiresApproval: true,
	})

	if result.Status != StatusDone {
		t.Errorf("expected done, got %s", result.Status)
	}
}

func TestExecute_NoApprover(t *testing.T) {
	m := NewManager(testLogger())

	result := m.Execute(context.Background(), ExecRequest{
		ID:               "no-approver",
		Command:          "echo",
		RequiresApproval: true,
	})

	if result.Status != StatusDenied {
		t.Errorf("expected denied with no approver, got %s", result.Status)
	}
}

func TestGet_And_List(t *testing.T) {
	m := NewManager(testLogger())

	m.Execute(context.Background(), ExecRequest{ID: "a", Command: "echo", Args: []string{"a"}})
	m.Execute(context.Background(), ExecRequest{ID: "b", Command: "echo", Args: []string{"b"}})

	snap := m.Get("a")
	if snap == nil {
		t.Error("expected process 'a'")
	}
	if snap != nil && snap.Status != StatusDone {
		t.Errorf("expected done, got %s", snap.Status)
	}
	if m.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent")
	}

	list := m.List()
	if len(list) != 2 {
		t.Errorf("expected 2 processes, got %d", len(list))
	}
}

func TestKill(t *testing.T) {
	m := NewManager(testLogger())

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
	m := NewManager(testLogger())
	err := m.Kill("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent process")
	}
}

func TestPrune(t *testing.T) {
	m := NewManager(testLogger())

	m.Execute(context.Background(), ExecRequest{ID: "old", Command: "echo"})
	time.Sleep(10 * time.Millisecond)

	pruned := m.Prune(1 * time.Millisecond)
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	if m.Get("old") != nil {
		t.Error("expected old process to be pruned")
	}
}

func TestAutoID(t *testing.T) {
	m := NewManager(testLogger())
	result := m.Execute(context.Background(), ExecRequest{Command: "echo"})
	if result.ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestExecuteBackground(t *testing.T) {
	m := NewManager(testLogger())
	id := m.ExecuteBackground(context.Background(), ExecRequest{
		Command: "echo",
		Args:    []string{"bg"},
	})
	if id == "" {
		t.Fatal("expected non-empty process ID")
	}

	// Poll until done (max 2s).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := m.Get(id)
		if snap != nil && snap.Status == StatusDone {
			if snap.Result == nil || snap.Result.Stdout == "" {
				t.Error("expected stdout from background process")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("background process did not complete in time")
}

func TestParallelDrain_LargeOutput(t *testing.T) {
	// Verify that large concurrent stdout+stderr doesn't deadlock.
	m := NewManager(testLogger())
	// Generate 128KB on both stdout and stderr simultaneously.
	result := m.Execute(context.Background(), ExecRequest{
		ID:        "parallel-drain",
		Command:   "bash",
		Args:      []string{"-c", "dd if=/dev/zero bs=1024 count=128 2>/dev/null | tr '\\0' 'A'; dd if=/dev/zero bs=1024 count=128 | tr '\\0' 'B' >&2"},
		TimeoutMs: 10000,
	})
	if result.Status != StatusDone {
		t.Errorf("expected done, got %s (error: %s)", result.Status, result.Error)
	}
	if len(result.Stdout) < 100000 {
		t.Errorf("expected large stdout, got %d bytes", len(result.Stdout))
	}
	if len(result.Stderr) < 100000 {
		t.Errorf("expected large stderr, got %d bytes", len(result.Stderr))
	}
}

func TestEnvCache(t *testing.T) {
	m := NewManager(testLogger())

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
