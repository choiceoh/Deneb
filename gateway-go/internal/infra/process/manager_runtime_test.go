package process

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func waitForProcess(t *testing.T, manager *Manager, id string, timeout time.Duration, ready func(*ProcessSnapshot) bool) *ProcessSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if snapshot := manager.Get(id); snapshot != nil && ready(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot := manager.Get(id)
	t.Fatalf("process %q did not reach expected state within %s; last snapshot: %+v", id, timeout, snapshot)
	return nil
}

func processTerminal(snapshot *ProcessSnapshot) bool {
	switch snapshot.Status {
	case StatusDone, StatusFailed, StatusKilled, StatusDenied:
		return true
	default:
		return false
	}
}

func TestExecuteBackgroundDetachesCallerCancellation(t *testing.T) {
	manager := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	id := manager.ExecuteBackground(ctx, ExecRequest{
		ID:        "detached-caller",
		Command:   "bash",
		Args:      []string{"-c", "sleep 0.15; printf survived"},
		TimeoutMs: 3000,
	})
	cancel()

	snapshot := waitForProcess(t, manager, id, 3*time.Second, processTerminal)
	if snapshot.Status != StatusDone {
		t.Fatalf("status = %s, want done after caller cancellation; result=%+v", snapshot.Status, snapshot.Result)
	}
	if snapshot.Result == nil || !strings.Contains(snapshot.Result.Stdout, "survived") {
		t.Fatalf("stdout = %+v, want survived", snapshot.Result)
	}
}

func TestExecute_ClearsRuntimeReferencesAtTerminal(t *testing.T) {
	manager := newTestManager(t)
	result := manager.Execute(context.Background(), ExecRequest{
		ID:      "release-runtime-refs",
		Command: "bash",
		Args:    []string{"-c", "printf done"},
	})
	if result.Status != StatusDone {
		t.Fatalf("status = %s, want done", result.Status)
	}

	manager.mu.RLock()
	tracked := manager.processes[result.ID]
	manager.mu.RUnlock()
	if tracked == nil {
		t.Fatal("completed process was not tracked")
	}
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	if tracked.cmd != nil || tracked.cancel != nil || tracked.stdin != nil ||
		tracked.stdoutBuf != nil || tracked.stderrBuf != nil {
		t.Fatalf("terminal process retained runtime references: cmd=%v cancel=%v stdin=%v stdout=%v stderr=%v",
			tracked.cmd != nil, tracked.cancel != nil, tracked.stdin != nil,
			tracked.stdoutBuf != nil, tracked.stderrBuf != nil)
	}
}

func TestManagerStopCancelsAndJoinsBackgroundProcess(t *testing.T) {
	manager := NewManager(testLogger())
	id := manager.ExecuteBackground(context.Background(), ExecRequest{
		ID:        "stop-running",
		Command:   "bash",
		Args:      []string{"-c", "sleep 30"},
		TimeoutMs: 60_000,
	})
	waitForProcess(t, manager, id, 2*time.Second, func(snapshot *ProcessSnapshot) bool {
		return snapshot.Status == StatusRunning
	})

	started := time.Now()
	manager.Stop()
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Stop took %s, want background cancellation and join within 3s", elapsed)
	}

	snapshot := manager.Get(id)
	if snapshot == nil || snapshot.Status != StatusKilled || snapshot.Result == nil {
		t.Fatalf("snapshot after Stop = %+v, want terminal killed result", snapshot)
	}
}

func TestManagerStopKillsTermIgnoringProcessGroup(t *testing.T) {
	// Child traps TERM; production WaitDelay is 5s before pipe teardown. Shrink
	// so this contract test still proves group SIGKILL without dominating the suite.
	prev := gracefulStopDelay
	gracefulStopDelay = 50 * time.Millisecond
	t.Cleanup(func() { gracefulStopDelay = prev })

	manager := NewManager(testLogger())
	t.Cleanup(manager.Stop)
	pidFile := t.TempDir() + "/process-group.pid"
	id := manager.ExecuteBackground(context.Background(), ExecRequest{
		ID:      "stop-process-group",
		Command: "bash",
		Args: []string{
			"-c",
			`trap '' TERM; printf '%s' "$$" > "$1"; (trap '' TERM; sleep 30) & wait`,
			"bash", pidFile,
		},
		TimeoutMs: 60_000,
	})
	waitForProcess(t, manager, id, 2*time.Second, func(snapshot *ProcessSnapshot) bool {
		return snapshot.Status == StatusRunning
	})

	var pid int
	pidDeadline := time.Now().Add(time.Second)
	for time.Now().Before(pidDeadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse process group pid %q: %v", data, err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("process did not publish its group pid")
	}

	manager.Stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		// Darwin can transiently report EPERM while a killed member remains in
		// the process group as a zombie. Keep polling: ESRCH is the portable
		// proof that the full group has disappeared.
		if err != nil && !errors.Is(err, syscall.EPERM) {
			t.Fatalf("probe process group %d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process group %d still exists after Manager.Stop", pid)
}

func TestManagerStopCancelsBackgroundApprovalWait(t *testing.T) {
	manager := NewManager(testLogger())
	approvalStarted := make(chan struct{})
	manager.SetApprover(func(ctx context.Context, _ ExecRequest) bool {
		close(approvalStarted)
		<-ctx.Done()
		return false
	})

	id := manager.ExecuteBackground(context.Background(), ExecRequest{
		ID:               "stop-approval-wait",
		Command:          "bash",
		Args:             []string{"-c", "printf should-not-run"},
		RequiresApproval: true,
	})
	select {
	case <-approvalStarted:
	case <-time.After(time.Second):
		t.Fatal("approval callback did not start")
	}

	manager.Stop()
	snapshot := manager.Get(id)
	if snapshot == nil || snapshot.Status != StatusKilled || snapshot.Result == nil {
		t.Fatalf("snapshot after approval cancellation = %+v, want killed result", snapshot)
	}
}

func TestManagerStopIsConcurrentSafe(t *testing.T) {
	manager := NewManager(testLogger())
	const callers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			<-start
			manager.Stop()
		}()
	}
	close(start)
	wg.Wait()
}

func TestNewManagerAcceptsNilLogger(t *testing.T) {
	manager := NewManager(nil)
	if manager.logger == nil {
		t.Fatal("NewManager(nil) left logger nil")
	}
	manager.Stop()
}

func TestExecuteBackgroundAfterStopIsImmediatelyTerminal(t *testing.T) {
	manager := NewManager(testLogger())
	manager.Stop()

	id := manager.ExecuteBackground(context.Background(), ExecRequest{
		ID:        "after-stop",
		Command:   "bash",
		Args:      []string{"-c", "sleep 30"},
		TimeoutMs: 60_000,
	})
	snapshot := manager.Get(id)
	if snapshot == nil || snapshot.Status != StatusKilled || snapshot.Result == nil {
		t.Fatalf("immediate snapshot = %+v, want terminal killed result", snapshot)
	}
}

func TestManagerStopRacingBackgroundRegistrationLeavesNoSurvivor(t *testing.T) {
	const attempts = 16
	for i := range attempts {
		manager := NewManager(testLogger())
		id := "stop-race-" + string(rune('a'+i))
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			manager.ExecuteBackground(context.Background(), ExecRequest{
				ID:        id,
				Command:   "bash",
				Args:      []string{"-c", "sleep 30"},
				TimeoutMs: 60_000,
			})
		}()
		go func() {
			defer wg.Done()
			<-start
			manager.Stop()
		}()
		close(start)
		wg.Wait()

		snapshot := manager.Get(id)
		if snapshot == nil || snapshot.Status != StatusKilled || snapshot.Result == nil {
			t.Fatalf("attempt %d snapshot = %+v, want pollable killed result", i, snapshot)
		}
	}
}

func TestManagerRuntimeDetachedContextPreservesValues(t *testing.T) {
	type contextKey struct{}
	runtime := newManagerRuntime()
	parent, cancelParent := context.WithTimeout(
		context.WithValue(context.Background(), contextKey{}, "trace-value"),
		10*time.Millisecond,
	)
	type observation struct {
		value       any
		hasDeadline bool
	}
	seen := make(chan observation, 1)
	stopped := make(chan struct{})
	if !runtime.goDetached(parent, func(ctx context.Context) {
		_, hasDeadline := ctx.Deadline()
		seen <- observation{value: ctx.Value(contextKey{}), hasDeadline: hasDeadline}
		<-ctx.Done()
		close(stopped)
	}) {
		t.Fatal("detached runtime registration rejected before Stop")
	}

	got := <-seen
	if got.value != "trace-value" || got.hasDeadline {
		t.Fatalf("detached context = %+v, want preserved value without caller deadline", got)
	}
	defer cancelParent()
	select {
	case <-stopped:
		t.Fatal("caller deadline reached detached manager context")
	case <-time.After(50 * time.Millisecond):
	}

	runtime.stop()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("manager runtime cancellation did not reach detached context")
	}
}
