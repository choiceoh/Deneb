package handlerminiapp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// EngineMeasure carries its denominator and explicit missingness to the app.
//
//deneb:wire
type EngineMeasure struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Available bool    `json:"available"`
	Samples   float64 `json:"samples"`
}

//deneb:wire
type EngineTrendPoint struct {
	SinceMs       int64   `json:"sinceMs"`
	UntilMs       int64   `json:"untilMs"`
	Runtime       string  `json:"runtime"`
	StepRate      float64 `json:"stepRate"`
	DecodeRate    float64 `json:"decodeRate"`
	Acceptance    float64 `json:"acceptance"`
	HasStep       bool    `json:"hasStep"`
	HasDecode     bool    `json:"hasDecode"`
	HasAcceptance bool    `json:"hasAcceptance"`
}

//deneb:wire
type EngineDiagnosticEvent struct {
	AtMs int64  `json:"atMs"`
	Kind string `json:"kind"`
}

// EngineCondition describes an actual joint decode cohort, not a cross-product
// invented from independent concurrency and graph-capacity distributions.
//
//deneb:wire
type EngineCondition struct {
	Timing        string  `json:"timing"`
	Sequences     string  `json:"sequences"`
	Context       string  `json:"context"`
	Cache         string  `json:"cache"`
	Steps         float64 `json:"steps"`
	StepRate      float64 `json:"stepRate"`
	TokensPerStep float64 `json:"tokensPerStep"`
	Acceptance    float64 `json:"acceptance"`
	DraftTokens   float64 `json:"draftTokens"`
}

//deneb:wire
type EngineDiagnosticWindow struct {
	Minutes         int                     `json:"minutes"`
	Runtime         string                  `json:"runtime"`
	ObservedSeconds float64                 `json:"observedSeconds"`
	Metrics         []EngineMeasure         `json:"metrics"`
	Stages          []EngineMeasure         `json:"stages"`
	Acceptance      []EngineMeasure         `json:"acceptance"`
	Latency         []EngineMeasure         `json:"latency"`
	Lengths         []EngineMeasure         `json:"lengths"`
	Conditions      []EngineCondition       `json:"conditions"`
	Points          []EngineTrendPoint      `json:"points"`
	Events          []EngineDiagnosticEvent `json:"events"`
}

//deneb:wire
type EngineDiagnosticsReport struct {
	LastSampleMs int64                    `json:"lastSampleMs"`
	Stale        bool                     `json:"stale"`
	Windows      []EngineDiagnosticWindow `json:"windows"`
}

func engineDiagnostics(rows []enginespeed.DiagnosticInterval, now time.Time, currentRuntime string) EngineDiagnosticsReport {
	out := EngineDiagnosticsReport{Stale: true, Windows: []EngineDiagnosticWindow{}}
	if len(rows) == 0 {
		return out
	}
	latest := rows[len(rows)-1]
	out.LastSampleMs, out.Stale = latest.UntilMs, now.UnixMilli()-latest.UntilMs > 45000 || latest.Event == "incomplete_sample"
	runtime := latest.Identity
	if currentRuntime != "" && runtime != currentRuntime {
		runtime, out.Stale = currentRuntime, true
	}
	for _, minutes := range []int{30, 1440} {
		out.Windows = append(out.Windows, diagnosticWindow(rows, now, minutes, runtime))
	}
	return out
}

func metric(values map[string]float64, name string, labels map[string]string) float64 {
	return values[observe.DiagnosticKey(name, labels)]
}

func ratio(key, label, unit string, numerator, denominator, samples float64) EngineMeasure {
	m := EngineMeasure{Key: key, Label: label, Unit: unit, Samples: samples, Available: denominator > 0}
	if m.Available {
		m.Value = numerator / denominator
	}
	return m
}

// pairedRatio distinguishes an absent numerator from a measured zero.
func pairedRatio(v map[string]float64, key, label, unit, numerator, denominator string, labels map[string]string, scale float64) EngineMeasure {
	n, hasN := v[observe.DiagnosticKey(numerator, labels)]
	d, hasD := v[observe.DiagnosticKey(denominator, labels)]
	samples := d
	if unit == "tok/s" || unit == "step/s" {
		samples = n
	}
	m := ratio(key, label, unit, scale*n, d, samples)
	m.Available = m.Available && hasN && hasD
	if !m.Available {
		m.Value = 0
	}
	return m
}

func decodeMeasures(v map[string]float64, seconds float64) []EngineMeasure {
	decode := map[string]string{"kind": "decode"}
	var tokens, stepDenom float64
	for key, value := range v {
		name, labels := observe.DiagnosticSeries(key)
		if name == "st:condition_steps_total" {
			if t, ok := v[observe.DiagnosticKey("st:condition_tokens_total", labels)]; ok {
				stepDenom += value
				tokens += t
			}
		}
	}
	generated, hasGenerated := v["vllm:generation_tokens_total"]
	if committed, ok := v["st:generation_tokens_committed_total"]; ok {
		generated = committed
		hasGenerated = true
	}
	if !hasGenerated {
		seconds = 0
	}
	return []EngineMeasure{
		pairedRatio(v, "steps", "호스트 디코드 회차", "step/s", "st:step_seconds_count", "st:step_seconds_sum", decode, 1),
		ratio("tokens_step", "완료 스텝당 출력", "tok/step", tokens, stepDenom, stepDenom),
		pairedRatio(v, "decode", "요청 디코드", "tok/s", "vllm:time_per_output_token_seconds_count", "vllm:time_per_output_token_seconds_sum", nil, 1),
		pairedRatio(v, "acceptance", "드래프트 수용률", "%", "vllm:spec_decode_num_accepted_tokens_total", "vllm:spec_decode_num_draft_tokens_total", nil, 100),
		ratio("throughput", "구간 합산 처리량", "tok/s", generated, seconds, generated),
		pairedRatio(v, "prefill", "실제 프리필", "tok/s", "st:prefill_computed_tokens_total", "st:prefill_compute_seconds_total", nil, 1),
	}
}

func diagnosticWindow(rows []enginespeed.DiagnosticInterval, now time.Time, minutes int, runtime string) EngineDiagnosticWindow {
	w := EngineDiagnosticWindow{
		Minutes: minutes, Runtime: runtime,
		Metrics: []EngineMeasure{}, Stages: []EngineMeasure{}, Acceptance: []EngineMeasure{},
		Latency: []EngineMeasure{}, Lengths: []EngineMeasure{}, Conditions: []EngineCondition{},
		Points: []EngineTrendPoint{}, Events: []EngineDiagnosticEvent{},
	}
	cutoff := now.Add(-time.Duration(minutes) * time.Minute).UnixMilli()
	values := map[string]float64{}
	var pointValues map[string]float64
	var point EngineTrendPoint
	bucketMs := int64(60000)
	if minutes > 30 {
		bucketMs = 15 * 60000
	}
	flushPoint := func() {
		if pointValues == nil {
			return
		}
		m := decodeMeasures(pointValues, 0)
		point.StepRate, point.HasStep = m[0].Value, m[0].Available
		point.DecodeRate, point.HasDecode = m[2].Value, m[2].Available
		point.Acceptance, point.HasAcceptance = m[3].Value, m[3].Available
		w.Points = append(w.Points, point)
		pointValues = nil
	}
	for _, row := range rows {
		// Do not proportionally invent counter values for a partial interval.
		if row.SinceMs < cutoff || row.UntilMs > now.UnixMilli() {
			continue
		}
		if row.Event != "" {
			flushPoint()
			w.Events = append(w.Events, EngineDiagnosticEvent{AtMs: row.UntilMs, Kind: row.Event})
			continue
		}
		if pointValues == nil || point.Runtime != row.Identity || point.UntilMs != row.SinceMs || point.SinceMs/bucketMs != row.SinceMs/bucketMs {
			flushPoint()
			point = EngineTrendPoint{SinceMs: row.SinceMs, Runtime: row.Identity}
			pointValues = map[string]float64{}
		}
		point.UntilMs = row.UntilMs
		for k, v := range row.Values {
			pointValues[k] += v
		}
		// Totals always belong to the latest runtime; earlier runs remain on
		// the timeline but are never silently averaged into the current build.
		if row.Identity == runtime {
			w.ObservedSeconds += row.Seconds
			for k, v := range row.Values {
				values[k] += v
			}
		}
	}
	flushPoint()
	w.Metrics = decodeMeasures(values, w.ObservedSeconds)
	stageSamples := values["st:decode_stage_samples_total"]
	var acceptedSegments float64
	for k, v := range values {
		n, _ := observe.DiagnosticSeries(k)
		if n == "st:spec_accepted_per_step_total" {
			acceptedSegments += v
		}
	}
	for k, v := range values {
		name, labels := observe.DiagnosticSeries(k)
		switch name {
		case "st:decode_stage_seconds_total":
			w.Stages = append(w.Stages, ratio(labels["stage"], labels["stage"], "ms/step", 1000*v, stageSamples, stageSamples))
		case "st:spec_accepted_per_step_total":
			w.Acceptance = append(w.Acceptance, ratio(labels["accepted"], labels["accepted"]+"개 수용", "%", 100*v, acceptedSegments, v))
		case "st:condition_steps_total":
			elapsed := metric(values, "st:condition_seconds_total", labels)
			if elapsed <= 0 || v <= 0 {
				continue
			}
			t, hasTokens := values[observe.DiagnosticKey("st:condition_tokens_total", labels)]
			if !hasTokens {
				continue
			}
			d := metric(values, "st:condition_drafted_total", labels)
			accepted, hasAccepted := values[observe.DiagnosticKey("st:condition_accepted_total", labels)]
			if !hasAccepted {
				d = 0
			}
			c := EngineCondition{
				Sequences: labels["sequences"], Context: labels["context"], Cache: labels["cache"], Timing: labels["timing"], Steps: v,
				StepRate: v / elapsed, TokensPerStep: t / v, DraftTokens: d,
			}
			if d > 0 {
				c.Acceptance = 100 * accepted / d
			}
			w.Conditions = append(w.Conditions, c)
		}
	}
	for _, spec := range []struct{ name, label string }{
		{"vllm:time_to_first_token_seconds", "첫 토큰"}, {"vllm:request_queue_time_seconds", "대기"}, {"vllm:e2e_request_latency_seconds", "전체 응답"},
	} {
		for _, q := range []float64{.5, .95} {
			v, ok := observe.HistogramQuantile(values, spec.name+"_bucket", nil, q)
			w.Latency = append(w.Latency, EngineMeasure{
				Key: fmt.Sprintf("%s_%g", spec.name, q), Label: fmt.Sprintf("%s P%.0f", spec.label, 100*q), Value: v * 1000, Unit: "ms", Available: ok,
				Samples: metric(values, spec.name+"_bucket", map[string]string{"le": "+Inf"}),
			})
		}
	}
	for _, spec := range []struct{ part, label string }{{"reasoning", "추론"}, {"answer", "답변"}} {
		labels := map[string]string{"part": spec.part}
		count := metric(values, "st:response_tokens_count", labels)
		w.Lengths = append(w.Lengths, pairedRatio(values, spec.part, spec.label+" 평균 길이", "tok", "st:response_tokens_sum", "st:response_tokens_count", labels, 1))
		for _, q := range []float64{.5, .95} {
			v, ok := observe.HistogramQuantile(values, "st:response_tokens_bucket", labels, q)
			w.Lengths = append(w.Lengths, EngineMeasure{Key: fmt.Sprintf("%s_%g", spec.part, q), Label: fmt.Sprintf("%s P%.0f", spec.label, q*100), Value: v, Unit: "tok", Available: ok, Samples: count})
		}
	}
	var finished float64
	for k, v := range values {
		n, _ := observe.DiagnosticSeries(k)
		if n == "st:response_finished_total" {
			finished += v
		}
	}
	w.Lengths = append(w.Lengths, ratio("length_limit", "토큰 제한 종료", "%", 100*metric(values, "st:response_finished_total", map[string]string{"reason": "length"}), finished, finished))
	sort.Slice(w.Stages, func(i, j int) bool { return w.Stages[i].Key < w.Stages[j].Key })
	sort.Slice(w.Acceptance, func(i, j int) bool { return w.Acceptance[i].Key < w.Acceptance[j].Key })
	sort.Slice(w.Conditions, func(i, j int) bool {
		a, b := w.Conditions[i], w.Conditions[j]
		return strings.Join([]string{a.Sequences, a.Context, a.Cache, a.Timing}, "/") < strings.Join([]string{b.Sequences, b.Context, b.Cache, b.Timing}, "/")
	})
	return w
}
