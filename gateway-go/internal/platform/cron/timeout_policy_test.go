package cron

import "testing"

func TestResolveCronJobTimeoutMs_DefaultSystemEvent(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "systemEvent", Text: "hello"}}
	got := ResolveCronJobTimeoutMs(job)
	if got != DefaultJobTimeoutMs {
		t.Fatalf("expected %d, got %d", DefaultJobTimeoutMs, got)
	}
}

func TestResolveCronJobTimeoutMs_DefaultAgentTurn(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "agentTurn", Message: "run"}}
	got := ResolveCronJobTimeoutMs(job)
	if got != AgentTurnSafetyTimeoutMs {
		t.Fatalf("expected %d, got %d", AgentTurnSafetyTimeoutMs, got)
	}
}

func TestResolveCronJobTimeoutMs_ExplicitTimeout(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "agentTurn", Message: "run", TimeoutSeconds: 120}}
	got := ResolveCronJobTimeoutMs(job)
	if got != 120000 {
		t.Fatalf("expected 120000, got %d", got)
	}
}
