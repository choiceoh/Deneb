package cron

import "testing"

func TestResolveCronJobTimeoutMs_DefaultSystemEvent(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "systemEvent", Text: "hello"}}
	got := ResolveCronJobTimeoutMs(job)
	if got != DefaultJobTimeoutMs {
		t.Fatalf("got %d, want %d", got, DefaultJobTimeoutMs)
	}
}

func TestResolveCronJobTimeoutMs_DefaultAgentTurn(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "agentTurn", Message: "run"}}
	got := ResolveCronJobTimeoutMs(job)
	if got != AgentTurnSafetyTimeoutMs {
		t.Fatalf("got %d, want %d", got, AgentTurnSafetyTimeoutMs)
	}
}
