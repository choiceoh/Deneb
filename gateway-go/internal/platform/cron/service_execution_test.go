package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type executionAgentFunc func(context.Context, AgentTurnParams) (string, error)

func (f executionAgentFunc) RunAgentTurn(ctx context.Context, params AgentTurnParams) (string, error) {
	return f(ctx, params)
}

// A cron run whose delivery handoff ERRORS must be recorded as status="error",
// not "ok". Before the fix the promote-to-error branch was dead — deliveryResult
// was only ever set with Delivered=true — so a failed handoff was logged "ok",
// the user silently lost the report, and consecutive failures never counted
// toward auto-disable. A bare handled=false with no error is an intentional
// suppression (the NO_REPLY / "nothing to report" noise floor) and must stay ok.
func TestExecuteJobStatusReflectsHandoffOutcomeWithErrorPromotion(t *testing.T) {
	baseJob := StoreJob{
		ID:       "j1",
		Name:     "report",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "hi"},
		Delivery: &JobDeliveryConfig{Channel: "client", To: "main"}, // not best-effort
	}

	cases := []struct {
		name         string
		handoff      func(ctx context.Context, channel, to, jobID, analysis string) (bool, error)
		wantStatus   string
		wantDelivery string // "none" | "failed" | "delivered"
	}{
		{
			name: "handoff error promotes to error",
			handoff: func(context.Context, string, string, string, string) (bool, error) {
				return false, errors.New("relay boom")
			},
			wantStatus:   "error",
			wantDelivery: "failed",
		},
		{
			name:         "intentional suppression stays ok",
			handoff:      func(context.Context, string, string, string, string) (bool, error) { return false, nil },
			wantStatus:   "ok",
			wantDelivery: "none",
		},
		{
			name:         "delivered stays ok",
			handoff:      func(context.Context, string, string, string, string) (bool, error) { return true, nil },
			wantStatus:   "ok",
			wantDelivery: "delivered",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, agent := newTestService(t)
			agent.output = "📬 일일 리포트 본문"
			svc.SetMainSessionHandoff(tc.handoff)
			if err := svc.store.AddJob(baseJob); err != nil {
				t.Fatal(err)
			}

			outcome, err := svc.Run(context.Background(), "j1", "manual")
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if outcome.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q (error=%q)", outcome.Status, tc.wantStatus, outcome.Error)
			}

			switch tc.wantDelivery {
			case "none":
				if outcome.Delivery != nil {
					t.Errorf("Delivery = %+v, want nil", outcome.Delivery)
				}
			case "failed":
				if outcome.Delivery == nil || outcome.Delivery.Delivered {
					t.Fatalf("Delivery = %+v, want a not-delivered result", outcome.Delivery)
				}
			case "delivered":
				if outcome.Delivery == nil || !outcome.Delivery.Delivered {
					t.Fatalf("Delivery = %+v, want a delivered result", outcome.Delivery)
				}
			}
		})
	}
}

func TestRunJobOnceEmitsFinishedEventAfterStateAndLogCommit(t *testing.T) {
	svc, agent := newTestService(t)
	agent.output = "완료 보고"
	job := StoreJob{
		ID:       "ordered",
		Name:     "ordered",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "run"},
		Delivery: &JobDeliveryConfig{Channel: "client", To: "main"},
		State:    JobState{ConsecutiveErrors: 2},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}

	var order []string
	stateCommitted := false
	logCommitted := false
	svc.SetMainSessionHandoff(func(context.Context, string, string, string, string) (bool, error) {
		order = append(order, "delivery")
		return true, nil
	})
	svc.OnEvent(func(event CronEvent) {
		switch event.Type {
		case "job_started":
			order = append(order, "started")
		case "job_finished":
			stored := svc.store.Job(job.ID)
			stateCommitted = stored != nil && stored.State.ConsecutiveErrors == 0 && stored.State.LastDeliveryStatus == "delivered"
			page := svc.runLog.ReadPage(job.ID, RunLogReadOpts{})
			logCommitted = len(page.Entries) == 1 && page.Entries[0].Status == "ok" && page.Entries[0].DeliveryStatus == "delivered"
			order = append(order, "finished")
		}
	})

	outcome := svc.runJobOnce(context.Background(), job, triggerManual)
	if outcome.Status != "ok" || outcome.Delivery == nil || !outcome.Delivery.Delivered {
		t.Fatalf("outcome = %+v", outcome)
	}
	if got := strings.Join(order, ","); got != "started,delivery,finished" {
		t.Fatalf("stage order = %q", got)
	}
	if !stateCommitted || !logCommitted {
		t.Fatalf("finished event observed state/log before commit: state=%v log=%v", stateCommitted, logCommitted)
	}
}

func TestRunJobOnce_CallerCancellationIsRecordedWithoutRetry(t *testing.T) {
	svc, _ := newTestService(t)
	svc.SetAgentRunner(executionAgentFunc(func(ctx context.Context, _ AgentTurnParams) (string, error) {
		return "", ctx.Err()
	}))
	job := StoreJob{
		ID:       "canceled",
		Name:     "canceled",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "run", RetryCount: 3, RetryBackoffMs: 1},
		Delivery: &JobDeliveryConfig{BestEffort: true},
	}
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := svc.runJobOnce(ctx, job, triggerManual)
	if outcome.Status != "error" || !strings.Contains(outcome.Error, context.Canceled.Error()) {
		t.Fatalf("canceled outcome = %+v", outcome)
	}
	if outcome.Retries != 0 {
		t.Fatalf("canceled retries = %d, want 0", outcome.Retries)
	}
	stored := svc.store.Job(job.ID)
	if stored == nil || stored.State.ConsecutiveErrors != 1 {
		t.Fatalf("canceled state = %+v", stored)
	}
	page := svc.runLog.ReadPage(job.ID, RunLogReadOpts{})
	if len(page.Entries) != 1 || page.Entries[0].Status != "error" {
		t.Fatalf("canceled run log = %+v", page.Entries)
	}
}
