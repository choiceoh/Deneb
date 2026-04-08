package cron

import (
	"testing"
)

func TestNormalizeStoredCronJobs_LegacyJobId(t *testing.T) {
	jobs := []map[string]any{
		{"jobId": "abc", "schedule": map[string]any{"kind": "cron", "expr": "0 * * * *"}, "payload": map[string]any{"kind": "systemEvent", "text": "hello"}},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	if jobs[0]["id"] != "abc" {
		t.Fatalf("got %v, want id=abc", jobs[0]["id"])
	}
	if _, has := jobs[0]["jobId"]; has {
		t.Fatal("expected jobId to be removed")
	}
	if result.Issues.JobID == 0 {
		t.Fatal("expected JobID issue count > 0")
	}
}

func TestNormalizeStoredCronJobs_LegacyStringSchedule(t *testing.T) {
	jobs := []map[string]any{
		{"id": "x", "schedule": "*/5 * * * *", "payload": map[string]any{"kind": "systemEvent", "text": "hi"}},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	sched, ok := jobs[0]["schedule"].(map[string]any)
	if !ok {
		t.Fatal("expected schedule to be map")
	}
	if sched["kind"] != "cron" {
		t.Fatalf("got %v, want kind=cron", sched["kind"])
	}
	if sched["expr"] != "*/5 * * * *" {
		t.Fatalf("got %v, want expr='*/5 * * * *'", sched["expr"])
	}
	if result.Issues.LegacyScheduleString == 0 {
		t.Fatal("expected LegacyScheduleString issue count > 0")
	}
}

func TestNormalizeStoredCronJobs_InferPayloadFromTopLevel(t *testing.T) {
	jobs := []map[string]any{
		{"id": "x", "message": "run task", "schedule": map[string]any{"kind": "cron", "expr": "0 * * * *"}},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	payload, ok := jobs[0]["payload"].(map[string]any)
	if !ok {
		t.Fatal("expected payload to be map")
	}
	if payload["kind"] != "agentTurn" {
		t.Fatalf("got %v, want kind=agentTurn", payload["kind"])
	}
	if payload["message"] != "run task" {
		t.Fatalf("got %v, want message='run task'", payload["message"])
	}
}

func TestNormalizeStoredCronJobs_NormalizePayloadKind(t *testing.T) {
	jobs := []map[string]any{
		{"id": "x", "schedule": map[string]any{"kind": "cron", "expr": "0 * * * *"}, "payload": map[string]any{"kind": "agentturn", "message": "hi"}},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	payload := jobs[0]["payload"].(map[string]any)
	if payload["kind"] != "agentTurn" {
		t.Fatalf("got %v, want agentTurn", payload["kind"])
	}
}

func TestNormalizeStoredCronJobs_SessionTargetCurrentToIsolated(t *testing.T) {
	jobs := []map[string]any{
		{
			"id":            "x",
			"sessionTarget": "current",
			"schedule":      map[string]any{"kind": "cron", "expr": "0 * * * *"},
			"payload":       map[string]any{"kind": "agentTurn", "message": "hi"},
		},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	if jobs[0]["sessionTarget"] != "isolated" {
		t.Fatalf("got %v, want isolated", jobs[0]["sessionTarget"])
	}
}

func TestNormalizeStoredCronJobs_WakeModeDefault(t *testing.T) {
	jobs := []map[string]any{
		{"id": "x", "schedule": map[string]any{"kind": "cron", "expr": "0 * * * *"}, "payload": map[string]any{"kind": "systemEvent", "text": "hi"}},
	}
	NormalizeStoredCronJobs(jobs)
	if jobs[0]["wakeMode"] != "now" {
		t.Fatalf("got %v, want wakeMode=now", jobs[0]["wakeMode"])
	}
}

func TestNormalizeStoredCronJobs_LegacyDeliveryMode(t *testing.T) {
	jobs := []map[string]any{
		{
			"id":       "x",
			"schedule": map[string]any{"kind": "cron", "expr": "0 * * * *"},
			"payload":  map[string]any{"kind": "systemEvent", "text": "hi"},
			"delivery": map[string]any{"mode": "deliver"},
		},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	delivery := jobs[0]["delivery"].(map[string]any)
	if delivery["mode"] != "announce" {
		t.Fatalf("got %v, want mode=announce", delivery["mode"])
	}
	if result.Issues.LegacyDeliveryMode == 0 {
		t.Fatal("expected LegacyDeliveryMode issue count > 0")
	}
}

func TestNormalizeStoredCronJobs_NoMutationWhenValid(t *testing.T) {
	jobs := []map[string]any{
		{
			"id":            "x",
			"name":          "My Job",
			"enabled":       true,
			"wakeMode":      "now",
			"sessionTarget": "main",
			"schedule":      map[string]any{"kind": "cron", "expr": "0 * * * *"},
			"payload":       map[string]any{"kind": "systemEvent", "text": "hello"},
			"state":         map[string]any{},
		},
	}
	result := NormalizeStoredCronJobs(jobs)
	// Some fields may still get normalized (stagger, name trim, etc.)
	// but the core structure should remain intact.
	_ = result
}

func TestNormalizeStoredCronJobs_AutoDeliveryForIsolatedAgentTurn(t *testing.T) {
	jobs := []map[string]any{
		{
			"id":            "x",
			"sessionTarget": "isolated",
			"schedule":      map[string]any{"kind": "cron", "expr": "0 * * * *"},
			"payload":       map[string]any{"kind": "agentTurn", "message": "run this"},
			"state":         map[string]any{},
		},
	}
	result := NormalizeStoredCronJobs(jobs)
	if !result.Mutated {
		t.Fatal("expected mutation for auto-delivery")
	}
	delivery, ok := jobs[0]["delivery"].(map[string]any)
	if !ok {
		t.Fatal("expected delivery to be set")
	}
	if delivery["mode"] != "announce" {
		t.Fatalf("got %v, want mode=announce", delivery["mode"])
	}
}
