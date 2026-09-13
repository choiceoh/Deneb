package observe

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// engineMetricsBody reproduces the exact shape the serving engine emits
// (stkernel engine/base/serve.py): vLLM series names, an engine="st" label
// rather than a per-model one, and st:step_seconds split by kind.
func engineMetricsBody(ttftSum, queueSum, tpotSum, tpotCount, e2eSum, promptTok, genTok, prefillStep, decodeStep float64, running, waiting int) string {
	var b strings.Builder
	line := func(series string, v float64) {
		fmt.Fprintf(&b, "%s %g\n", series, v)
	}
	b.WriteString("# HELP vllm:time_to_first_token_seconds arrival to first token, queueing included\n")
	b.WriteString("# TYPE vllm:time_to_first_token_seconds histogram\n")
	line(`vllm:time_to_first_token_seconds_bucket{engine="st",le="0.25"}`, 3)
	line(`vllm:time_to_first_token_seconds_sum{engine="st"}`, ttftSum)
	line(`vllm:time_to_first_token_seconds_count{engine="st"}`, 10)
	line(`vllm:request_queue_time_seconds_sum{engine="st"}`, queueSum)
	line(`vllm:time_per_output_token_seconds_sum{engine="st"}`, tpotSum)
	line(`vllm:time_per_output_token_seconds_count{engine="st"}`, tpotCount)
	line(`vllm:e2e_request_latency_seconds_sum{engine="st"}`, e2eSum)
	line(`vllm:e2e_request_latency_seconds_count{engine="st"}`, 10)
	line(`vllm:prompt_tokens_total{engine="st"}`, promptTok)
	line(`vllm:generation_tokens_total{engine="st"}`, genTok)
	line(`vllm:num_requests_running{engine="st"}`, float64(running))
	line(`vllm:num_requests_waiting{engine="st"}`, float64(waiting))
	// Split by kind, exactly as the engine writes it — both halves are the
	// engine stepping, so a busy-time total has to add them.
	line(`st:step_seconds_sum{engine="st",kind="prefill"}`, prefillStep)
	line(`st:step_seconds_sum{engine="st",kind="decode"}`, decodeStep)
	// A series whose name merely starts the same must not be folded in.
	line(`vllm:prompt_tokens_total_created{engine="st"}`, 1.7e9)
	return b.String()
}

func engineServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[{"id":"glm-5.3-flash","object":"model"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The scrape has to read the engine's real shape: labels it does not key on,
// one series split across labels, and a longer name sharing a prefix.
func TestFetchEngineCounters_ReadsTheEnginesOwnShape(t *testing.T) {
	srv := engineServer(t, engineMetricsBody(4.0, 1.0, 20.0, 1000, 60.0, 8000, 1000, 3.0, 27.0, 2, 1))

	got, ok := FetchEngineCounters(context.Background(), srv.URL+"/metrics")
	if !ok {
		t.Fatal("a live engine must be readable")
	}
	if got.Model != "glm-5.3-flash" {
		t.Errorf("Model = %q — the endpoint names the model, since the series do not", got.Model)
	}
	// Both kinds of step are the engine being busy.
	if got.BusySeconds != 30 {
		t.Errorf("BusySeconds = %v, want 30 (prefill 3 + decode 27)", got.BusySeconds)
	}
	if got.PromptTokens != 8000 {
		t.Errorf("PromptTokens = %v, want 8000 — *_created must not fold in", got.PromptTokens)
	}
	if got.Concurrency() != 3 {
		t.Errorf("Concurrency() = %d, want 3 (2 running + 1 waiting)", got.Concurrency())
	}
}

// An endpoint that is not an engine, or is simply down, must report "no
// measurement" — never a zeroed one that a day would then average in.
func TestFetchEngineCounters_NonEngineEndpointsYieldNoSample(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "# nothing we know\n")
	}))
	t.Cleanup(empty.Close)
	if _, ok := FetchEngineCounters(context.Background(), empty.URL+"/metrics"); ok {
		t.Error("an endpoint with none of the series must not count as a sample")
	}

	// The private-host rule is re-applied at the scrape, so no call path can
	// aim this at a public address.
	if _, ok := FetchEngineCounters(context.Background(), "http://metrics.example.com/metrics"); ok {
		t.Error("a public host must be refused")
	}
}

// A restart resets the engine's counters to zero. The difference across it is
// not a delta, and a rate computed from it is fiction.
func TestEngineDeltaBetween_RefusesAnIntervalContainingARestart(t *testing.T) {
	before := EngineCounters{TTFTSeconds: 100, TTFTCount: 50, PromptTokens: 9000, GenerationTokens: 4000}
	after := EngineCounters{TTFTSeconds: 2, TTFTCount: 1, PromptTokens: 300, GenerationTokens: 50}

	if _, ok := EngineDeltaBetween(before, after); ok {
		t.Fatal("counters that moved backwards must not produce a delta")
	}
	if _, ok := EngineDeltaBetween(before, before); !ok {
		t.Fatal("an idle interval is still a valid (zero) delta")
	}
}

// The three rates, each from its own denominator.
func TestEngineDelta_Rates(t *testing.T) {
	// 8,000 prompt tokens whose TTFT summed to 4s, of which 1s was queue —
	// so 3s of prefill. 1,000 decode steps summing 20s of per-token time.
	// 60 request-seconds resident over 30s of engine stepping.
	d := EngineDelta{
		TTFTSeconds: 4, QueueSeconds: 1,
		TPOTSeconds: 20, TPOTCount: 1000,
		E2ESeconds: 60, BusySeconds: 30,
		Requests: 10, PromptTokens: 8000, GenerationTokens: 1000,
	}
	got := d.Rates()

	if math.Abs(got.PrefillTokensPerSec-8000.0/3.0) > 0.01 {
		t.Errorf("prefill = %.2f, want %.2f — the queue wait comes out of TTFT", got.PrefillTokensPerSec, 8000.0/3.0)
	}
	if math.Abs(got.DecodeTokensPerSec-50) > 0.01 {
		t.Errorf("decode = %.2f, want 50 (1000 steps / 20s)", got.DecodeTokensPerSec)
	}
	if math.Abs(got.ConcurrencyWhileBusy-2) > 0.01 {
		t.Errorf("concurrency = %.2f, want 2 (60 request-seconds over 30 busy seconds)", got.ConcurrencyWhileBusy)
	}
}

// Idle time must not dilute the concurrency figure: a day with the same work
// spread over more wall clock reports the same average, because the
// denominator is the engine's busy time and not the day.
func TestEngineDelta_ConcurrencyExcludesIdleTime(t *testing.T) {
	busy := EngineDelta{E2ESeconds: 60, BusySeconds: 30}.Rates()
	if math.Abs(busy.ConcurrencyWhileBusy-2) > 0.01 {
		t.Fatalf("concurrency = %.2f, want 2", busy.ConcurrencyWhileBusy)
	}
	// An engine that did nothing has no concurrency to report — not a zero
	// average that would drag a week's figure down.
	if idle := (EngineDelta{E2ESeconds: 0, BusySeconds: 0}).Rates(); idle.Measured() {
		t.Error("a day with no work must report no measurement")
	}
}

// Deltas add, which is what lets a day be the sum of its intervals.
func TestEngineDelta_Add(t *testing.T) {
	a := EngineDelta{TTFTSeconds: 1, PromptTokens: 100, Requests: 1}
	b := EngineDelta{TTFTSeconds: 3, PromptTokens: 300, Requests: 2}
	got := a.Add(b)
	if got.TTFTSeconds != 4 || got.PromptTokens != 400 || got.Requests != 3 {
		t.Fatalf("Add = %+v", got)
	}
}
