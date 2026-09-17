package observe

import (
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// EngineDiagnostics retains labels that the daily totals intentionally fold.
// Only bounded operational dimensions are accepted; request IDs and text never
// enter the history. Missing series remain missing, including real zero values.
type EngineDiagnostics struct {
	Identity  string             `json:"identity,omitempty"`
	Truncated bool               `json:"truncated,omitempty"`
	Values    map[string]float64 `json:"values,omitempty"`
}

var diagnosticNames = map[string]bool{
	"st:step_seconds_sum": true, "st:step_seconds_count": true,
	"st:decode_emitted_tokens_total":       true,
	"st:generation_tokens_committed_total": true,
	"st:decode_stage_seconds_total":        true, "st:decode_stage_samples_total": true,
	"st:spec_accepted_per_step_total":    true,
	"st:decode_steps_by_sequences_total": true, "st:decode_capacity_bucket_total": true,
	"st:condition_steps_total": true, "st:condition_seconds_total": true,
	"st:condition_tokens_total": true, "st:condition_drafted_total": true,
	"st:condition_accepted_total":      true,
	"st:prefill_computed_tokens_total": true, "st:prefill_compute_seconds_total": true,
	"st:response_tokens_sum": true, "st:response_tokens_count": true,
	"st:response_tokens_bucket": true, "st:response_finished_total": true,
	engineTPOTCountMetric: true, engineTPOTSumMetric: true,
	engineGenTokensMetric: true, engineSpecDraftMetric: true, engineSpecAcceptedMetric: true,
	"vllm:spec_decode_num_drafts_total": true,
	enginePrefixQueriesMetric:           true, enginePrefixHitsMetric: true,
	"vllm:time_to_first_token_seconds_bucket": true,
	"vllm:request_queue_time_seconds_bucket":  true,
	"vllm:e2e_request_latency_seconds_bucket": true,
}

// DiagnosticKey is stable across Prometheus label order and encoding.
func DiagnosticKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name += "|" + k + "=" + url.QueryEscape(labels[k])
	}
	return name
}

func DiagnosticSeries(key string) (string, map[string]string) {
	parts := strings.Split(key, "|")
	labels := map[string]string{}
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if ok {
			labels[k], _ = url.QueryUnescape(v)
		}
	}
	return parts[0], labels
}

// readDiagnostic shares the existing scrape; it performs no extra HTTP call.
func readDiagnostic(line string, out *EngineDiagnostics) {
	i := strings.IndexAny(line, "{ \t")
	if i < 1 || strings.HasPrefix(line, "#") {
		return
	}
	name := line[:i]
	identity := name == "st:runtime_info" || name == "st:lane_info"
	if !diagnosticNames[name] && !identity {
		return
	}
	_, value, ok := parseVllmCounter(line, name)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return
	}
	body := ""
	if line[i] == '{' {
		end := strings.IndexByte(line[i:], '}')
		if end < 0 {
			return
		}
		body = line[i+1 : i+end]
	}
	labels := map[string]string{}
	for _, k := range []string{"kind", "stage", "accepted", "sequences", "capacity", "context", "cache", "timing", "part", "reason", "le"} {
		if v := promLabel(body, k); v != "" && len(v) <= 64 {
			labels[k] = v
		}
	}
	if identity {
		for _, k := range []string{"build", "boot", "k", "precision", "model"} {
			if v := promLabel(body, k); v != "" && len(v) <= 96 {
				labels[k] = v
			}
		}
		if name == "st:runtime_info" || out.Identity == "" {
			out.Identity = DiagnosticKey(name, labels)
		}
		return
	}
	// A bounded parser even when an endpoint exports malformed cardinality.
	if out.Truncated {
		return
	}
	if out.Values == nil {
		out.Values = map[string]float64{}
	}
	key := DiagnosticKey(name, labels)
	if _, exists := out.Values[key]; exists || len(out.Values) < 2048 {
		out.Values[key] += value
	} else {
		out.Truncated = true
		out.Values = nil
	}
}

// HistogramQuantile uses interval bucket deltas, not a percentile of means.
// Overflow is an unknown upper tail rather than an invented finite latency.
func HistogramQuantile(values map[string]float64, name string, labels map[string]string, q float64) (float64, bool) {
	type bucket struct{ upper, count float64 }
	var buckets []bucket
	for key, count := range values {
		n, ls := DiagnosticSeries(key)
		if n != name {
			continue
		}
		match := true
		for k, v := range labels {
			if ls[k] != v {
				match = false
			}
		}
		if !match {
			continue
		}
		upper, err := strconv.ParseFloat(ls["le"], 64)
		if err == nil {
			buckets = append(buckets, bucket{upper, count})
		}
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].upper < buckets[j].upper })
	if len(buckets) < 2 || !math.IsInf(buckets[len(buckets)-1].upper, 1) || q <= 0 || q >= 1 {
		return 0, false
	}
	total := buckets[len(buckets)-1].count
	if total <= 0 {
		return 0, false
	}
	for i := 1; i < len(buckets); i++ {
		if buckets[i].count < buckets[i-1].count {
			return 0, false
		}
	}
	var lower, previous float64
	for _, b := range buckets {
		if b.count < previous {
			return 0, false
		}
		if b.count >= q*total {
			if math.IsInf(b.upper, 1) || b.count == previous {
				return 0, false
			}
			return lower + (b.upper-lower)*(q*total-previous)/(b.count-previous), true
		}
		lower, previous = b.upper, b.count
	}
	return 0, false
}
