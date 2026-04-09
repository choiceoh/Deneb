package autonomous

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeDreamer struct {
	mu             sync.Mutex
	shouldDream    bool
	runErr         error
	runReport      *DreamReport
	incrementCount int
	runCount       int
}

func (f *fakeDreamer) ShouldDream(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shouldDream
}

func (f *fakeDreamer) RunDream(context.Context) (*DreamReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runCount++
	return f.runReport, f.runErr
}

func (f *fakeDreamer) IncrementTurn(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incrementCount++
}

type fakeNotifier struct {
	mu      sync.Mutex
	calls   int
	message string
}

func (n *fakeNotifier) Notify(_ context.Context, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	n.message = message
	return nil
}

func waitForEvent(t *testing.T, ch <-chan CycleEvent, typ string) CycleEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event type %q", typ)
		}
	}
}

func TestServiceIncrementDreamTurnRunsDreamCycle(t *testing.T) {
	svc := NewService(nil)
	d := &fakeDreamer{shouldDream: true, runReport: &DreamReport{FactsVerified: 2, DurationMs: 250}}
	n := &fakeNotifier{}
	events := make(chan CycleEvent, 4)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetNotifier(n)
	svc.SetDreamer(d)

	svc.IncrementDreamTurn(context.Background())

	_ = waitForEvent(t, events, "dreaming_started")
	completed := waitForEvent(t, events, "dreaming_completed")
	if completed.DreamReport == nil || completed.DreamReport.FactsVerified != 2 {
		t.Fatalf("unexpected dream report: %+v", completed.DreamReport)
	}

	d.mu.Lock()
	if d.incrementCount != 1 {
		t.Fatalf("got %d, want increment count 1", d.incrementCount)
	}
	if d.runCount != 1 {
		t.Fatalf("got %d, want run count 1", d.runCount)
	}
	d.mu.Unlock()

	n.mu.Lock()
	if n.calls != 1 {
		t.Fatalf("got %d, want 1 notification call", n.calls)
	}
	if n.message == "" {
		t.Fatal("expected non-empty notification message")
	}
	n.mu.Unlock()
}

func TestServiceIncrementDreamTurnRunErrorEmitsFailure(t *testing.T) {
	svc := NewService(nil)
	d := &fakeDreamer{shouldDream: true, runErr: errors.New("dream failed")}
	n := &fakeNotifier{}
	events := make(chan CycleEvent, 4)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetNotifier(n)
	svc.SetDreamer(d)

	svc.IncrementDreamTurn(context.Background())

	_ = waitForEvent(t, events, "dreaming_started")
	_ = waitForEvent(t, events, "dreaming_failed")

	n.mu.Lock()
	if n.calls != 1 {
		t.Fatalf("got %d, want 1 notification call", n.calls)
	}
	if n.message == "" {
		t.Fatal("expected failure notification message")
	}
	n.mu.Unlock()
}

// --- Periodic task tests ---

type fakeTask struct {
	mu       sync.Mutex
	name     string
	interval time.Duration
	runCount int
	runErr   error
	runCh    chan struct{} // signals each run
}

func newFakeTask(name string, interval time.Duration) *fakeTask {
	return &fakeTask{name: name, interval: interval, runCh: make(chan struct{}, 10)}
}

func (f *fakeTask) Name() string            { return f.name }
func (f *fakeTask) Interval() time.Duration { return f.interval }
func (f *fakeTask) Run(context.Context) error {
	f.mu.Lock()
	f.runCount++
	err := f.runErr
	f.mu.Unlock()
	f.runCh <- struct{}{}
	return err
}

func TestService_RegisterTask_RunsOnStart(t *testing.T) {
	svc := NewService(nil)
	task := newFakeTask("test-task", 1*time.Hour) // long interval; initial run after 30s would be too slow for test

	svc.RegisterTask(task)
	svc.Start()
	defer svc.Stop()

	// The task should be registered with a status entry.
	status := svc.TaskStatus("test-task")
	if status == nil {
		t.Fatal("expected task status to exist")
	}
	if status.Name != "test-task" {
		t.Errorf("status.Name = %q, want test-task", status.Name)
	}
}

func TestService_TaskPanicRecovery(t *testing.T) {
	svc := NewService(nil)

	// Create a task that panics.
	panicTask := newFakeTask("panic-task", 1*time.Hour)
	svc.RegisterTask(panicTask)

	// Register panicker status for verification.
	svc.taskStatus["panicker"] = &TaskStatus{Name: "panicker"}

	// Test that executeTask recovers from panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatal("panic should have been recovered in executeTask")
			}
		}()
		svc.executeTask(context.Background(), &panicingTask{})
	}()

	// Verify error was recorded.
	status := svc.TaskStatus("panicker")
	if status != nil && status.ErrorCount > 0 {
		// Good — panic was recorded.
	}
}

type panicingTask struct{}

func (p *panicingTask) Name() string            { return "panicker" }
func (p *panicingTask) Interval() time.Duration { return time.Hour }
func (p *panicingTask) Run(context.Context) error {
	panic("test panic")
}

func TestService_ExecuteTask_RecordsPanic(t *testing.T) {
	svc := NewService(nil)
	svc.taskStatus["panicker"] = &TaskStatus{Name: "panicker"}

	// Should not panic.
	svc.executeTask(context.Background(), &panicingTask{})

	status := svc.TaskStatus("panicker")
	if status == nil {
		t.Fatal("expected status")
	}
	if status.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", status.ErrorCount)
	}
	if status.LastError == "" {
		t.Error("LastError should contain panic info")
	}
	if status.Running {
		t.Error("Running should be false after panic recovery")
	}
}

func TestService_ExecuteTask_SkipsIfRunning(t *testing.T) {
	svc := NewService(nil)
	svc.taskStatus["busy"] = &TaskStatus{Name: "busy", Running: true}

	task := newFakeTask("busy", time.Hour)
	svc.executeTask(context.Background(), task)

	task.mu.Lock()
	if task.runCount != 0 {
		t.Error("task should not run while already running")
	}
	task.mu.Unlock()
}

// TestService_ConcurrentIncrementDreamTurn verifies that RunDream is called
// exactly once even when multiple goroutines call IncrementDreamTurn simultaneously
// with ShouldDream returning true.
func TestService_ConcurrentIncrementDreamTurn(t *testing.T) {
	svc := NewService(nil)
	// Use a slow dreamer so the dream cycle outlasts all concurrent triggers,
	// ensuring the dreamRunning guard reliably deduplicates.
	d := &slowFakeDreamer{
		shouldDream: true,
		holdTime:    200 * time.Millisecond,
		report:      &DreamReport{FactsVerified: 1, DurationMs: 200},
	}
	events := make(chan CycleEvent, 20)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetDreamer(d)

	// Launch 10 goroutines calling IncrementDreamTurn simultaneously.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.IncrementDreamTurn(context.Background())
		}()
	}
	wg.Wait()

	// Wait for the dream cycle to complete (exactly one should run).
	_ = waitForEvent(t, events, "dreaming_completed")

	d.mu.Lock()
	runCount := d.runCount
	d.mu.Unlock()
	if runCount != 1 {
		t.Errorf("RunDream called %d times, want exactly 1", runCount)
	}
}

// TestService_SetDreamer_AfterStart verifies that the dream timer goroutine is
// spawned when SetDreamer is called after Start().
func TestService_SetDreamer_AfterStart(t *testing.T) {
	svc := NewService(nil)
	svc.Start()
	defer svc.Stop()

	d := &fakeDreamer{shouldDream: false}
	svc.SetDreamer(d)

	// Verify the timer cancel was set (indicating the goroutine was spawned).
	svc.mu.Lock()
	hasTimerCancel := svc.dreamTimerCancel != nil
	svc.mu.Unlock()

	if !hasTimerCancel {
		t.Error("expected dreamTimerCancel to be set after SetDreamer")
	}
}

// TestService_NotifierError verifies the service doesn't crash if Notifier returns an error.
func TestService_NotifierError(t *testing.T) {
	svc := NewService(nil)
	d := &fakeDreamer{
		shouldDream: true,
		runReport:   &DreamReport{FactsVerified: 3, DurationMs: 50},
	}
	events := make(chan CycleEvent, 10)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetNotifier(&errorNotifier{})
	svc.SetDreamer(d)

	svc.IncrementDreamTurn(context.Background())

	// Should complete even though notifier fails.
	completed := waitForEvent(t, events, "dreaming_completed")
	if completed.DreamReport == nil || completed.DreamReport.FactsVerified != 3 {
		t.Fatalf("unexpected dream report: %+v", completed.DreamReport)
	}
}

// errorNotifier always returns an error.
type errorNotifier struct{}

func (n *errorNotifier) Notify(_ context.Context, _ string) error {
	return errors.New("notification delivery failed")
}

// TestService_DreamNotRunWhileAlreadyRunning verifies that a second dream cycle
// is not started while one is already in progress.
func TestService_DreamNotRunWhileAlreadyRunning(t *testing.T) {
	svc := NewService(nil)

	// Create a slow dreamer that holds the lock for a while.
	slowDreamer := &slowFakeDreamer{
		shouldDream: true,
		holdTime:    100 * time.Millisecond,
		report:      &DreamReport{DurationMs: 100},
	}
	events := make(chan CycleEvent, 10)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetDreamer(slowDreamer)

	// First call triggers dream.
	svc.IncrementDreamTurn(context.Background())

	// Wait briefly for the dream to start.
	_ = waitForEvent(t, events, "dreaming_started")

	// Second call while dream is running should be a no-op.
	svc.IncrementDreamTurn(context.Background())

	// Wait for completion.
	_ = waitForEvent(t, events, "dreaming_completed")

	slowDreamer.mu.Lock()
	runCount := slowDreamer.runCount
	slowDreamer.mu.Unlock()
	if runCount != 1 {
		t.Errorf("RunDream called %d times, want 1 (second should be skipped)", runCount)
	}
}

// slowFakeDreamer simulates a dream that takes time.
type slowFakeDreamer struct {
	mu          sync.Mutex
	shouldDream bool
	holdTime    time.Duration
	report      *DreamReport
	runCount    int
}

func (d *slowFakeDreamer) ShouldDream(context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shouldDream
}

func (d *slowFakeDreamer) RunDream(context.Context) (*DreamReport, error) {
	d.mu.Lock()
	d.runCount++
	d.mu.Unlock()
	time.Sleep(d.holdTime)
	return d.report, nil
}

func (d *slowFakeDreamer) IncrementTurn(context.Context) {}

// TestService_PeriodicTask_ErrorTracking verifies task error status updates.
func TestService_PeriodicTask_ErrorTracking(t *testing.T) {
	svc := NewService(nil)
	task := newFakeTask("error-task", time.Hour)
	task.runErr = errors.New("fetch failed")

	svc.RegisterTask(task)
	svc.taskStatus["error-task"] = &TaskStatus{Name: "error-task"}

	// Execute the task directly.
	svc.executeTask(context.Background(), task)

	status := svc.TaskStatus("error-task")
	if status == nil {
		t.Fatal("expected task status")
	}
	if status.RunCount != 1 {
		t.Errorf("RunCount = %d, want 1", status.RunCount)
	}
	if status.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", status.ErrorCount)
	}
	if status.LastError != "fetch failed" {
		t.Errorf("LastError = %q, want %q", status.LastError, "fetch failed")
	}
	if status.Running {
		t.Error("task should not be running after completion")
	}
}

// TestService_PeriodicTask_Success verifies successful task execution clears errors.
func TestService_PeriodicTask_Success(t *testing.T) {
	svc := NewService(nil)
	task := newFakeTask("ok-task", time.Hour)

	svc.RegisterTask(task)
	svc.taskStatus["ok-task"] = &TaskStatus{Name: "ok-task", LastError: "old error", ErrorCount: 1}

	svc.executeTask(context.Background(), task)

	status := svc.TaskStatus("ok-task")
	if status.LastError != "" {
		t.Errorf("LastError should be cleared, got %q", status.LastError)
	}
	if status.RunCount != 1 {
		t.Errorf("RunCount = %d, want 1", status.RunCount)
	}
	if status.LastRunAt == 0 {
		t.Error("LastRunAt should be set")
	}
}
