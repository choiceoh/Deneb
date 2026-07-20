package toolport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlackboardPlanBeginEndFailClosed(t *testing.T) {
	b := NewBlackboard()
	if err := b.Plan([]StepContract{
		{ID: "extract", Goal: "pull order fields", Outputs: []string{"order_id", "phone"}},
		{ID: "notify", Goal: "send sms", Inputs: []string{"order_id", "phone"}, Outputs: []string{"sent"}},
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if _, err := b.BeginStep("notify"); err == nil || !strings.Contains(err.Error(), "missing required keys") {
		t.Fatalf("BeginStep before inputs: err=%v", err)
	}

	if _, err := b.BeginStep("extract"); err != nil {
		t.Fatalf("BeginStep extract: %v", err)
	}
	if got := b.ActiveStep(); got != "extract" {
		t.Fatalf("ActiveStep=%q", got)
	}
	if err := b.EndStep("extract", map[string]json.RawMessage{
		"order_id": json.RawMessage(`"A-100"`),
		// phone missing
	}); err == nil || !strings.Contains(err.Error(), "missing outputs") {
		t.Fatalf("EndStep incomplete: err=%v", err)
	}
	if err := b.EndStep("extract", map[string]json.RawMessage{
		"order_id": json.RawMessage(`"A-100"`),
		"phone":    json.RawMessage(`"01012345678"`),
		"extra":    json.RawMessage(`1`),
	}); err == nil || !strings.Contains(err.Error(), "undeclared output") {
		t.Fatalf("EndStep undeclared: err=%v", err)
	}
	if err := b.EndStep("extract", map[string]json.RawMessage{
		"order_id": json.RawMessage(`"A-100"`),
		"phone":    json.RawMessage(`"01012345678"`),
	}); err != nil {
		t.Fatalf("EndStep extract: %v", err)
	}

	inputs, err := b.BeginStep("notify")
	if err != nil {
		t.Fatalf("BeginStep notify: %v", err)
	}
	if string(inputs["order_id"]) != `"A-100"` || string(inputs["phone"]) != `"01012345678"` {
		t.Fatalf("inputs=%v", inputs)
	}
	if err := b.EndStep("notify", map[string]json.RawMessage{"sent": json.RawMessage(`true`)}); err != nil {
		t.Fatalf("EndStep notify: %v", err)
	}
	if got := b.ActiveStep(); got != "" {
		t.Fatalf("ActiveStep after end=%q", got)
	}
}

func TestBlackboardRequireAndPut(t *testing.T) {
	b := NewBlackboard()
	if err := b.Put("price_jd", json.RawMessage(`12900`), "manual"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := b.Get("price_jd")
	if !ok || string(got.Value) != "12900" || got.Source != "manual" {
		t.Fatalf("Get=%v ok=%v", got, ok)
	}
	if _, err := b.Require([]string{"price_jd", "price_tb"}); err == nil || !strings.Contains(err.Error(), "price_tb") {
		t.Fatalf("Require missing: err=%v", err)
	}
	vals, err := b.Require([]string{"price_jd"})
	if err != nil || string(vals["price_jd"]) != "12900" {
		t.Fatalf("Require ok: vals=%v err=%v", vals, err)
	}
}

func TestBlackboardRejectsBadKeysAndValues(t *testing.T) {
	b := NewBlackboard()
	if err := b.Put("bad-key", json.RawMessage(`1`), ""); err == nil {
		t.Fatal("expected bad key rejection")
	}
	if err := b.Put("ok", json.RawMessage(`{`), ""); err == nil {
		t.Fatal("expected invalid JSON rejection")
	}
	if err := b.Plan([]StepContract{{ID: "noop"}}); err == nil {
		t.Fatal("expected empty I/O rejection")
	}
}
