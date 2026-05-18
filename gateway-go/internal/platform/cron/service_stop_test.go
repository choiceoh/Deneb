package cron

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingAgent runs until ctx is cancelled. It records whether the
// final return path was ctx.Done() (clean cancellation) or a deadline
// the test imposed (timed out without cancellation, which would mean
// StopCtx failed to propagate).
type blockingAgent struct {
	started   chan struct{}
	finishMu  sync.Mutex
	finished  bool
	cancelled atomic.Bool
}

func (a *blockingAgent) RunAgentTurn(ctx context.Context, _ AgentTurnParams) (string, error) {
	select {
	case <-a.started:
	default:
		close(a.started)
	}
	<-ctx.Done()
	a.cancelled.Store(true)
	a.finishMu.Lock()
	a.finished = true
	a.finishMu.Unlock()
	return "", ctx.Err()
}

// TestStopCtxCancelsInFlightEnqueueRun guards issue #1633: graceful
// shutdown must cancel async EnqueueRun jobs and wait for them to exit
// before returning, so doShutdown does not tear down dependencies
// (Telegram plugin, chat handler) while a cron run is still using them.
func TestStopCtxCancelsInFlightEnqueueRun(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	agent := &blockingAgent{started: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(ServiceConfig{StorePath: storePath, Enabled: true}, agent, logger)
	svc.idleInterval = 200 * time.Millisecond
	svc.errBackoff = 100 * time.Millisecond
	svc.minLoopGap = 5 * time.Millisecond

	// Register a job so EnqueueRun has something to look up.
	if err := svc.store.AddJob(StoreJob{
		ID:       "long-running",
		Name:     "long-running",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "block"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
		State:    JobState{NextRunAtMs: time.Now().UnixMilli() + 60_000},
	}); err != nil {
		t.Fatal(err)
	}

	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	if err := svc.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Enqueue an async run and wait for the agent to actually start.
	if err := svc.EnqueueRun(context.Background(), "long-running", "now"); err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	select {
	case <-agent.started:
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not start within 2s; EnqueueRun never reached the agent")
	}

	// Now Stop with a bounded deadline. The agent is blocked on ctx.Done()
	// — without StopCtx cancelling the run context, this would time out.
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelStop()

	stopReturned := make(chan struct{})
	go func() {
		svc.StopCtx(stopCtx)
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("StopCtx did not return within 3s; in-flight wait deadlocked")
	}

	if !agent.cancelled.Load() {
		t.Error("agent did not observe ctx cancellation; runCtx was not propagated")
	}
	agent.finishMu.Lock()
	finished := agent.finished
	agent.finishMu.Unlock()
	if !finished {
		t.Error("agent did not finish before StopCtx returned; inFlight wait was not awaited")
	}
}

// TestStopCtxDeadlineBudget verifies the deadline contract: when an
// executor refuses to respect cancellation, StopCtx still returns
// promptly so a stuck agent turn cannot block gateway shutdown forever.
func TestStopCtxDeadlineBudget(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	// Agent that ignores ctx and sleeps a long time.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(ServiceConfig{StorePath: storePath, Enabled: true}, &stuckAgent{}, logger)
	svc.idleInterval = 200 * time.Millisecond
	svc.errBackoff = 100 * time.Millisecond
	svc.minLoopGap = 5 * time.Millisecond

	if err := svc.store.AddJob(StoreJob{
		ID:       "stuck",
		Name:     "stuck",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "stuck"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
		State:    JobState{NextRunAtMs: time.Now().UnixMilli() + 60_000},
	}); err != nil {
		t.Fatal(err)
	}

	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	if err := svc.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := svc.EnqueueRun(context.Background(), "stuck", "now"); err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	// Give the goroutine a chance to start.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelStop()

	start := time.Now()
	svc.StopCtx(stopCtx)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("StopCtx blocked for %v; deadline budget not respected", elapsed)
	}
}

// stuckAgent ignores ctx and sleeps for 10s. Used to verify that
// StopCtx returns before that deadline fires when its own ctx expires.
type stuckAgent struct{}

func (stuckAgent) RunAgentTurn(_ context.Context, _ AgentTurnParams) (string, error) {
	time.Sleep(10 * time.Second)
	return "", nil
}
