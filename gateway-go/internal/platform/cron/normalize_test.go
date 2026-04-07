package cron

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestNormalizeRequiredName(t *testing.T) {
	name := testutil.Must(NormalizeRequiredName("  My Job  "))
	if name != "My Job" {
		t.Errorf("expected 'My Job', got %q", name)
	}

	_, err := NormalizeRequiredName("")
	if err == nil {
		t.Error("expected error for empty name")
	}

	_, err = NormalizeRequiredName("   ")
	if err == nil {
		t.Error("expected error for blank name")
	}
}

func TestInferLegacyName_FromPayload(t *testing.T) {
	job := StoreJob{
		Payload: StorePayload{Kind: "agentTurn", Message: "Run the backup script\nand do other stuff"},
	}
	name := InferLegacyName(job)
	if name != "Run the backup script" {
		t.Errorf("expected 'Run the backup script', got %q", name)
	}
}

func TestInferLegacyName_FromSchedule(t *testing.T) {
	job := StoreJob{
		Schedule: StoreSchedule{Kind: "cron", Expr: "0 */2 * * *"},
		Payload:  StorePayload{Kind: "systemEvent"},
	}
	name := InferLegacyName(job)
	if name != "Cron: 0 */2 * * *" {
		t.Errorf("expected 'Cron: 0 */2 * * *', got %q", name)
	}
}

func TestInferLegacyName_Every(t *testing.T) {
	job := StoreJob{
		Schedule: StoreSchedule{Kind: "every", EveryMs: 30000},
		Payload:  StorePayload{Kind: "systemEvent"},
	}
	name := InferLegacyName(job)
	if name != "Every: 30000ms" {
		t.Errorf("expected 'Every: 30000ms', got %q", name)
	}
}

func TestInferLegacyName_At(t *testing.T) {
	job := StoreJob{
		Schedule: StoreSchedule{Kind: "at"},
		Payload:  StorePayload{Kind: "systemEvent"},
	}
	name := InferLegacyName(job)
	if name != "One-shot" {
		t.Errorf("expected 'One-shot', got %q", name)
	}
}

func TestInferLegacyName_Fallback(t *testing.T) {
	job := StoreJob{Payload: StorePayload{Kind: "systemEvent"}}
	name := InferLegacyName(job)
	if name != "Cron job" {
		t.Errorf("expected 'Cron job', got %q", name)
	}
}

func TestNormalizePayloadToSystemText(t *testing.T) {
	p1 := StorePayload{Kind: "systemEvent", Text: "  hello  "}
	if NormalizePayloadToSystemText(p1) != "hello" {
		t.Error("expected trimmed text for systemEvent")
	}

	p2 := StorePayload{Kind: "agentTurn", Message: "  run this  "}
	if NormalizePayloadToSystemText(p2) != "run this" {
		t.Error("expected trimmed message for agentTurn")
	}
}
