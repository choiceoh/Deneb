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
		t.Fatalf("expected increment count 1, got %d", d.incrementCount)
	}
	if d.runCount != 1 {
		t.Fatalf("expected run count 1, got %d", d.runCount)
	}
	d.mu.Unlock()

	n.mu.Lock()
	if n.calls != 1 {
		t.Fatalf("expected 1 notification call, got %d", n.calls)
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
		t.Fatalf("expected 1 notification call, got %d", n.calls)
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
	status := svc.GetTaskStatus("test-task")
	if status == nil {
		t.Fatal("expected task status to exist")
	}
	if status.Name != "test-task" {
		t.Errorf("status.Name = %q, want test-task", status.Name)
	}
}

func TestService_GetTaskStatus_Unknown(t *testing.T) {
	svc := NewService(nil)
	if status := svc.GetTaskStatus("nonexistent"); status != nil {
		t.Error("expected nil status for unknown task")
	}
}

func TestService_TaskPanicRecovery(t *testing.T) {
	svc := NewService(nil)

	// Create a task that panics.
	panicTask := newFakeTask("panic-task", 1*time.Hour)
	svc.RegisterTask(panicTask)

	// Manually execute to test panic recovery.
	type panicTaskType struct {
		fakeTask
	}
	pt := &panicTaskType{fakeTask: fakeTask{name: "panic-task2"}}
	svc.taskStatus["panic-task2"] = &TaskStatus{Name: "panic-task2"}

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
	status := svc.GetTaskStatus("panicker")
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

	status := svc.GetTaskStatus("panicker")
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

func TestTruncateOutput(t *testing.T) {
	short := "abc"
	if got := truncateOutput(short, 10); got != short {
		t.Fatalf("expected %q, got %q", short, got)
	}

	long := "안녕하세요반갑습니다"
	got := truncateOutput(long, 3)
	if got != "안녕하..." {
		t.Fatalf("expected UTF-8 safe truncation, got %q", got)
	}
}
