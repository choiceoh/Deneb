package handlerminiapp

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func TestEngineDiagnosticsSeparatesRunsAndTimingScopes(t *testing.T) {
	now := time.Date(2026, 9, 17, 2, 0, 0, 0, time.UTC)
	values := map[string]float64{
		observe.DiagnosticKey("st:step_seconds_count", map[string]string{"kind": "decode"}): 20,
		observe.DiagnosticKey("st:step_seconds_sum", map[string]string{"kind": "decode"}):   1,
		"vllm:spec_decode_num_draft_tokens_total":                                           100,
		"vllm:spec_decode_num_accepted_tokens_total":                                        0,
	}
	for _, scope := range []string{"sync_wall", "async_residency"} {
		labels := map[string]string{"sequences": "4", "context": "32768", "cache": "hit", "timing": scope}
		values[observe.DiagnosticKey("st:condition_steps_total", labels)] = 10
		values[observe.DiagnosticKey("st:condition_seconds_total", labels)] = 2
		values[observe.DiagnosticKey("st:condition_tokens_total", labels)] = 30
	}
	rows := []enginespeed.DiagnosticInterval{
		{Identity: "old", SinceMs: now.Add(-time.Minute).UnixMilli(), UntilMs: now.Add(-45 * time.Second).UnixMilli(), Seconds: 15, Values: map[string]float64{"vllm:spec_decode_num_draft_tokens_total": 900}},
		{Identity: "new", SinceMs: now.Add(-30 * time.Second).UnixMilli(), UntilMs: now.Add(-30 * time.Second).UnixMilli(), Event: "runtime_changed"},
		{Identity: "new", SinceMs: now.Add(-30 * time.Second).UnixMilli(), UntilMs: now.Add(-15 * time.Second).UnixMilli(), Seconds: 15, Values: values},
	}
	r := engineDiagnostics(rows, now, "")
	w := r.Windows[0]
	if r.Stale || w.ObservedSeconds != 15 || w.Runtime != "new" || len(w.Conditions) != 2 || len(w.Events) != 1 || len(w.Points) != 2 {
		t.Fatalf("bad history %+v", r)
	}
	if w.Metrics[0].Value != 20 || w.Metrics[1].Value != 3 || !w.Metrics[3].Available || w.Metrics[3].Value != 0 || w.Metrics[3].Samples != 100 {
		t.Fatalf("bad rates %+v", w.Metrics)
	}
	if w.Metrics[5].Available {
		t.Fatal("invented prefill metric")
	}
	if !engineDiagnostics(rows, now.Add(time.Minute), "").Stale {
		t.Fatal("old data still fresh")
	}
	changed := engineDiagnostics(rows, now, "next-boot")
	if !changed.Stale || changed.Windows[0].ObservedSeconds != 0 {
		t.Fatal("live runtime reused previous boot's rates")
	}
}

func TestEngineDiagnosticsMissingNumeratorIsUnknown(t *testing.T) {
	values := map[string]float64{"vllm:spec_decode_num_draft_tokens_total": 10, "st:prefill_compute_seconds_total": 1, "vllm:time_per_output_token_seconds_sum": 1, "st:step_seconds_sum|kind=decode": 1}
	rates := decodeMeasures(values, 15)
	if rates[0].Available || rates[2].Available || rates[3].Available || rates[5].Available {
		t.Fatalf("missing numerator became zero: %+v", rates)
	}
	values["vllm:spec_decode_num_accepted_tokens_total"] = 0
	if !decodeMeasures(values, 15)[3].Available {
		t.Fatal("measured zero lost")
	}
}
