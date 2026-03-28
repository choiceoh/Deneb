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
