package cron

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubAgent counts agent turns and lets tests synchronize on completion.
// RunAgentTurn returns immediately so tests don't wait for real LLM calls.
type stubAgent struct {
	mu      sync.Mutex
	calls   int32
	jobIDs  []string
	output  string
	err     error
	delayMs int
}

func (a *stubAgent) RunAgentTurn(ctx context.Context, params AgentTurnParams) (string, error) {
	if a.delayMs > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(a.delayMs) * time.Millisecond):
		}
	}
	a.mu.Lock()
	a.jobIDs = append(a.jobIDs, params.SessionKey)
	a.mu.Unlock()
	atomic.AddInt32(&a.calls, 1)
	return a.output, a.err
}

func (a *stubAgent) callCount() int {
	return int(atomic.LoadInt32(&a.calls))
}

// newTestService spins up a Service backed by a tempdir store, an empty agent
// stub, and a discard logger. Loop tunables are tightened so tests don't spend
// real time waiting.
func newTestService(t *testing.T) (*Service, *stubAgent) {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	agent := &stubAgent{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(ServiceConfig{StorePath: storePath, Enabled: true}, agent, logger)
	// Tighten timing so tests are fast without losing semantics.
	svc.idleInterval = 200 * time.Millisecond
	svc.errBackoff = 100 * time.Millisecond
	svc.minLoopGap = 5 * time.Millisecond
	return svc, agent
}

// waitFor polls cond every 5ms up to the deadline. Returns true if cond
// returned true before timeout.
func waitFor(deadline time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestSchedulerLoopFiresDueJobs verifies the basic happy path: a due job
// gets executed by the loop without explicit wake signals.
func TestSchedulerLoopFiresDueJobs(t *testing.T) {
	svc, agent := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Job due immediately (NextRunAtMs in the past).
	now := time.Now().UnixMilli()
	job := StoreJob{
		ID:       "j1",
		Name:     "test",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "hi"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
		State:    JobState{NextRunAtMs: now - 1000},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	if !waitFor(2*time.Second, func() bool { return agent.callCount() >= 1 }) {
		t.Fatalf("expected agent run, got calls=%d", agent.callCount())
	}
}

// TestSchedulerLoopSurvivesEmptyJobs verifies the loop stays alive when there
// are zero enabled jobs. Previously this killed the chain via the nextWake==0
// early-return; now the loop sleeps idleInterval and keeps running.
func TestSchedulerLoopSurvivesEmptyJobs(t *testing.T) {
	svc, agent := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Sleep past one idleInterval. Loop must still be alive after this.
	time.Sleep(svc.idleInterval + 50*time.Millisecond)

	// Inject a due job directly into the store (bypassing Add's "future
	// NextRunAtMs" computation) and signal the loop. The loop must still
	// be alive after the empty-state period and pick it up.
	now := time.Now().UnixMilli()
	job := StoreJob{
		ID:       "j1",
		Name:     "test",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "hi"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
		State:    JobState{NextRunAtMs: now - 1000},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	svc.signalWake()

	if !waitFor(2*time.Second, func() bool { return agent.callCount() >= 1 }) {
		t.Fatalf("loop didn't pick up job after idle period; calls=%d", agent.callCount())
	}
}

// TestSchedulerLoopSurvivesStoreError exercises the errBackoff path. We can't
// inject a store error directly with the real Store, so we sanity-check that
// signalWake while the loop is alive doesn't panic and that subsequent
// Add still fires the job. A stronger fault-injection test would require
// a store interface; this catches the regression-class at minimum.
func TestSchedulerLoopSurvivesStoreError(t *testing.T) {
	svc, agent := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Hammer signalWake while the loop is sleeping. cap=1 channel must
	// not panic, deadlock, or starve subsequent legitimate work.
	for range 50 {
		svc.signalWake()
	}

	// Inject a due job directly and signal once more. The loop must fire it.
	now := time.Now().UnixMilli()
	job := StoreJob{
		ID:       "j1",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "hi"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
		State:    JobState{NextRunAtMs: now - 1000},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	svc.signalWake()
	if !waitFor(2*time.Second, func() bool { return agent.callCount() >= 1 }) {
		t.Fatalf("loop didn't fire after signal storm; calls=%d", agent.callCount())
	}
}

// TestSignalWakeCoalesces verifies that many signals while the loop is busy
// produce at most one extra wake (channel cap 1). Sanity check that the
// non-blocking send pattern doesn't deadlock under load.
func TestSignalWakeCoalesces(t *testing.T) {
	svc, _ := newTestService(t)
	// Don't Start — we want to test the channel directly without the loop
	// draining wakes.
	for range 1000 {
		svc.signalWake()
	}
	// Drain once; any subsequent recv should block (no second pending wake).
	select {
	case <-svc.wakeCh:
	default:
		t.Fatal("expected at least one pending wake")
	}
	select {
	case <-svc.wakeCh:
		t.Fatal("wakes did not coalesce; cap-1 channel had >1 pending")
	case <-time.After(20 * time.Millisecond):
	}
}

// TestManualRunPreservesFutureNextRun reproduces the 2026-04-28 morning-letter
// bug: running a job manually at 22:00 yesterday must NOT advance the next
// scheduled fire (today's 08:00) to tomorrow.
//
// This is the core fix — applyJobResult now preserves a future NextRunAtMs
// when trigger == triggerManual.
func TestManualRunPreservesFutureNextRun(t *testing.T) {
	svc, _ := newTestService(t)

	now := time.Now().UnixMilli()
	futureFire := now + 6*60*60*1000 // 6h in the future — like today's 08:00 from 02:00

	job := StoreJob{
		ID:       "morning-letter",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "cron", Expr: "0 8 * * *"},
		Payload:  StorePayload{Kind: "agentTurn", Message: "morning"},
		State:    JobState{NextRunAtMs: futureFire},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	// Simulate a manual run with a successful outcome.
	outcome := RunOutcome{
		Status:    "ok",
		StartedAt: now,
		EndedAt:   now,
	}
	svc.applyJobResult(job, outcome, "session-x", triggerManual)

	got := svc.store.Job("morning-letter")
	if got == nil {
		t.Fatal("job vanished")
	}
	if got.State.NextRunAtMs != futureFire {
		t.Errorf("manual run advanced future NextRunAtMs: before=%d after=%d (should be preserved)",
			futureFire, got.State.NextRunAtMs)
	}
}

// TestManualRunOverdueAdvances verifies the inverse: when NextRunAtMs is in
// the past at manual-run time (i.e. the job was overdue and the operator
// nudged it through), we DO advance to the next match. Otherwise stale
// past timestamps would persist.
func TestManualRunOverdueAdvances(t *testing.T) {
	svc, _ := newTestService(t)

	now := time.Now().UnixMilli()
	past := now - 60_000

	job := StoreJob{
		ID:       "overdue",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 3_600_000}, // 1h
		Payload:  StorePayload{Kind: "agentTurn", Message: "x"},
		State:    JobState{NextRunAtMs: past},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	outcome := RunOutcome{Status: "ok", StartedAt: now, EndedAt: now}
	svc.applyJobResult(job, outcome, "session-y", triggerManual)

	got := svc.store.Job("overdue")
	if got.State.NextRunAtMs <= now {
		t.Errorf("overdue manual run did not advance NextRunAtMs: before=%d after=%d (should be future)",
			past, got.State.NextRunAtMs)
	}
}

// TestSchedulerTriggerSchedulerAlwaysAdvances verifies that scheduler-driven
// runs (the normal path) always advance NextRunAtMs to the next match,
// regardless of where the previous value sat.
func TestSchedulerTriggerAlwaysAdvances(t *testing.T) {
	svc, _ := newTestService(t)

	now := time.Now().UnixMilli()
	futureFire := now + 6*60*60*1000

	job := StoreJob{
		ID:       "j1",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "x"},
		State:    JobState{NextRunAtMs: futureFire},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	outcome := RunOutcome{Status: "ok", StartedAt: now, EndedAt: now}
	svc.applyJobResult(job, outcome, "session-z", triggerScheduler)

	got := svc.store.Job("j1")
	if got.State.NextRunAtMs == futureFire {
		t.Errorf("scheduler trigger did not advance NextRunAtMs (should always advance, got preserved %d)",
			got.State.NextRunAtMs)
	}
}

// TestStartStopIdempotent verifies that the worker goroutine cleanly exits
// on Stop and a subsequent Start spins up a fresh loop.
func TestStartStopIdempotent(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("second Start (no-op): %v", err)
	}
	svc.Stop()
	svc.Stop() // idempotent

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start after Stop: %v", err)
	}
	svc.Stop()
}

// TestStopWaitsForLoopExit verifies Stop blocks until the worker goroutine
// has actually exited, not just signaled.
func TestStopWaitsForLoopExit(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	loopDone := svc.loopDone
	svc.Stop()

	select {
	case <-loopDone:
	default:
		t.Fatal("Stop returned before loopDone closed")
	}
}

// TestSignalWakeNoLoopIsSafe verifies signalWake doesn't deadlock when the
// loop hasn't started (e.g. early-init code paths or after Stop).
func TestSignalWakeNoLoopIsSafe(t *testing.T) {
	svc, _ := newTestService(t)
	// No Start.
	for range 100 {
		svc.signalWake()
	}
}

// TestApplyJobResultSignalsWake verifies that applyJobResult nudges the loop
// so a freshly-computed NextRunAtMs sooner than the current sleep target
// is honored without waiting for idleInterval.
func TestApplyJobResultSignalsWake(t *testing.T) {
	svc, _ := newTestService(t)

	// Drain any pending wakes.
	select {
	case <-svc.wakeCh:
	default:
	}

	job := StoreJob{
		ID:       "j1",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "x"},
		State:    JobState{NextRunAtMs: time.Now().UnixMilli()},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	outcome := RunOutcome{Status: "ok"}
	svc.applyJobResult(job, outcome, "k", triggerScheduler)

	select {
	case <-svc.wakeCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("applyJobResult did not signal wake")
	}
}

// errStr is exercised indirectly above; this is a smoke test that it doesn't
// panic on nil and round-trips error text.
func TestErrStr(t *testing.T) {
	if errStr(nil) != "" {
		t.Error("errStr(nil) should be empty")
	}
	if errStr(errors.New("boom")) != "boom" {
		t.Error("errStr(errors.New) should round-trip")
	}
}
