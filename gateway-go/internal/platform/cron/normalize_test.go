package cron

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestNormalizeRequiredName(t *testing.T) {
	name := testutil.Must(NormalizeRequiredName("  My Job  "))
	if name != "My Job" {
		t.Errorf("got %q, want 'My Job'", name)
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
