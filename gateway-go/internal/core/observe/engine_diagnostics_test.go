package observe

import (
	"fmt"
	"math"
	"testing"
)

func TestDiagnosticLabelsAndMissingness(t *testing.T) {
	var d EngineDiagnostics
	for _, line := range []string{
		`st:step_seconds_sum{engine="st",kind="decode"} 2`,
		`st:step_seconds_sum{kind="prefill",engine="st"} 9`,
		`st:condition_steps_total{timing="device_burst",context="32768",sequences="4",cache="hit"} 0`,
		`st:runtime_info{build="abc",boot="123",k="7",precision="w4"} 1`,
		`st:lane_info{ar="oneshot"} 1`,
		`st:step_seconds_count{kind="decode"} NaN`,
		`st:step_seconds_count{kind="decode"} -1`,
		`st:step_seconds_count{kind="decode"} +Inf`,
	} {
		readDiagnostic(line, &d)
	}
	if d.Values[DiagnosticKey("st:step_seconds_sum", map[string]string{"kind": "decode"})] != 2 || len(d.Values) != 3 {
		t.Fatalf("dimensions/zero/nonfinite: %+v", d)
	}
	if d.Identity != "st:runtime_info|boot=123|build=abc|k=7|precision=w4" {
		t.Fatal(d.Identity)
	}
}

func TestDiagnosticHistogramQuantiles(t *testing.T) {
	v := map[string]float64{}
	for bound, count := range map[string]float64{"0": 0, "1": 50, "2": 90, "4": 100, "+Inf": 100} {
		v[DiagnosticKey("latency_bucket", map[string]string{"le": bound})] = count
	}
	for q, want := range map[float64]float64{.5: 1, .95: 3} {
		got, ok := HistogramQuantile(v, "latency_bucket", nil, q)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Fatalf("q=%g got=%g/%v", q, got, ok)
		}
	}
	v[DiagnosticKey("latency_bucket", map[string]string{"le": "+Inf"})] = 300
	if _, ok := HistogramQuantile(v, "latency_bucket", nil, .95); ok {
		t.Fatal("overflow tail was presented as finite")
	}
	v[DiagnosticKey("latency_bucket", map[string]string{"le": "4"})] = 1
	if _, ok := HistogramQuantile(v, "latency_bucket", nil, .5); ok {
		t.Fatal("nonmonotonic buckets accepted")
	}
}

func TestDiagnosticOverflowRejectsWholeSample(t *testing.T) {
	var d EngineDiagnostics
	for i := 0; i < 2050; i++ {
		readDiagnostic(fmt.Sprintf(`st:condition_steps_total{context="%d"} 1`, i), &d)
	}
	if !d.Truncated || len(d.Values) != 0 {
		t.Fatalf("partial sample retained: %v/%d", d.Truncated, len(d.Values))
	}
}
