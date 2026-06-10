// pending_rerun_test.go — PendingRerun semantics: runs lost to restarts
// (aborted turns) or overlapping triggers (per-job guard skips) are re-run
// instead of silently dropped (the 2026-06-10 restart-storm incident).
package cron

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// seqAgent is a per-test stub whose first call can block until released,
// recording every call. Later calls return immediately with output "done".
type seqAgent struct {
	mu      sync.Mutex
	calls   int
	err     error         // returned by every call when set
	block   chan struct{} // first call waits on this when non-nil
	started chan struct{} // signaled once per call start
}

func (a *seqAgent) RunAgentTurn(ctx context.Context, params AgentTurnParams) (string, error) {
	a.mu.Lock()
	a.calls++
	call := a.calls
	blk := a.block
	a.mu.Unlock()
	if a.started != nil {
		a.started <- struct{}{}
	}
	if call == 1 && blk != nil {
		select {
		case <-blk:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if a.err != nil {
		return "", a.err
	}
	return "done", nil
}

func (a *seqAgent) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func newRerunTestService(t *testing.T, agent AgentRunner) *Service {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(ServiceConfig{StorePath: storePath, Enabled: true}, agent, logger)
	svc.idleInterval = 200 * time.Millisecond
	svc.errBackoff = 100 * time.Millisecond
	svc.minLoopGap = 5 * time.Millisecond
	return svc
}

func rerunTestJob() StoreJob {
	return StoreJob{
		ID:       "mail",
		Name:     "mail",
		Enabled:  false, // trigger-only, like the mail-watch job
		Schedule: StoreSchedule{Kind: "every", EveryMs: 3_600_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "analyze"},
		Delivery: &JobDeliveryConfig{BestEffort: true},
	}
}

// An aborted turn must be recorded status="aborted" (not ok, not error):
// PendingRerun queued for the boot scan, no consecutive-error count, and no
// immediate post-run rerun (the gateway is shutting down).
func TestAbortedRunQueuesPendingRerun(t *testing.T) {
	agent := &seqAgent{err: ErrTurnAborted}
	svc := newRerunTestService(t, agent)
	job := rerunTestJob()
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	outcome := svc.executeJobFullWithTrigger(context.Background(), job, triggerManual)

	if outcome.Status != "aborted" {
		t.Fatalf("status = %q, want aborted", outcome.Status)
	}
	if got := agent.callCount(); got != 1 {
		t.Fatalf("agent calls = %d, want 1 (no immediate rerun on abort)", got)
	}
	stored := svc.store.Job(job.ID)
	if stored == nil || !stored.State.PendingRerun {
		t.Fatalf("PendingRerun not persisted: %+v", stored)
	}
	if stored.State.ConsecutiveErrors != 0 {
		t.Fatalf("ConsecutiveErrors = %d, want 0 (abort is infra churn)", stored.State.ConsecutiveErrors)
	}
}

// Service.Start must re-run a job whose PendingRerun flag survived a restart —
// including a disabled (trigger-only) job — and clear the flag exactly once.
func TestStartRecoversPendingRerun(t *testing.T) {
	agent := &seqAgent{}
	svc := newRerunTestService(t, agent)
	job := rerunTestJob()
	job.State.PendingRerun = true
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	if !waitFor(2*time.Second, func() bool { return agent.callCount() >= 1 }) {
		t.Fatal("pending-rerun job never executed after Start")
	}
	if !waitFor(2*time.Second, func() bool {
		fresh := svc.store.Job(job.ID)
		return fresh != nil && !fresh.State.PendingRerun
	}) {
		t.Fatal("PendingRerun flag not cleared after boot recovery")
	}
}

// A trigger dropped by the per-job overlap guard must queue a rerun that the
// owning executor consumes after it finishes — two agent runs total, flag
// cleared at the end.
func TestOverlapSkipQueuesRerunConsumedPostRun(t *testing.T) {
	agent := &seqAgent{
		block:   make(chan struct{}),
		started: make(chan struct{}, 4),
	}
	svc := newRerunTestService(t, agent)
	job := rerunTestJob()
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	done := make(chan RunOutcome, 1)
	go func() {
		done <- svc.executeJobFullWithTrigger(context.Background(), job, triggerManual)
	}()

	// Wait until run #1 is inside the agent, then fire the overlapping trigger.
	select {
	case <-agent.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run never started")
	}
	skipped := svc.executeJobFullWithTrigger(context.Background(), job, triggerManual)
	if skipped.Status != "skipped" {
		t.Fatalf("overlap status = %q, want skipped", skipped.Status)
	}
	if stored := svc.store.Job(job.ID); stored == nil || !stored.State.PendingRerun {
		t.Fatal("overlap skip did not queue PendingRerun")
	}

	close(agent.block) // release run #1 → its post-run loop must consume the flag

	select {
	case outcome := <-done:
		if outcome.Status != "ok" {
			t.Fatalf("final status = %q, want ok", outcome.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owning executor never finished")
	}
	if got := agent.callCount(); got != 2 {
		t.Fatalf("agent calls = %d, want 2 (original + consumed rerun)", got)
	}
	if stored := svc.store.Job(job.ID); stored == nil || stored.State.PendingRerun {
		t.Fatal("PendingRerun flag not cleared after consumption")
	}
}
