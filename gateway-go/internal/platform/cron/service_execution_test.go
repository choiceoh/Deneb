package cron

import (
	"context"
	"errors"
	"testing"
)

// A cron run whose delivery handoff ERRORS must be recorded as status="error",
// not "ok". Before the fix the promote-to-error branch was dead — deliveryResult
// was only ever set with Delivered=true — so a failed handoff was logged "ok",
// the user silently lost the report, and consecutive failures never counted
// toward auto-disable. A bare handled=false with no error is an intentional
// suppression (the NO_REPLY / "nothing to report" noise floor) and must stay ok.
func TestExecuteJob_HandoffOutcomeStatus(t *testing.T) {
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
