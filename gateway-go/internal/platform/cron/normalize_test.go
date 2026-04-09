package cron

import (
	"testing"
)

func TestInferLegacyName_FromPayload(t *testing.T) {
	job := StoreJob{
		Payload: StorePayload{Kind: "agentTurn", Message: "Run the backup script\nand do other stuff"},
	}
	name := InferLegacyName(job)
	if name != "Run the backup script" {
		t.Errorf("got %q, want 'Run the backup script'", name)
	}
}

func TestInferLegacyName_FromSchedule(t *testing.T) {
	job := StoreJob{
		Schedule: StoreSchedule{Kind: "cron", Expr: "0 */2 * * *"},
		Payload:  StorePayload{Kind: "systemEvent"},
	}
	name := InferLegacyName(job)
	if name != "Cron: 0 */2 * * *" {
		t.Errorf("got %q, want 'Cron: 0 */2 * * *'", name)
	}
}
