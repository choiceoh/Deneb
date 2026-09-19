package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

func TestEngineMetricsURLDerivesLocalAndRejectsPublicHosts(t *testing.T) {
	cases := map[string]string{
		// Self-hosted shapes → /metrics (with /v1 stripped).
		"http://127.0.0.1:8000/v1":       "http://127.0.0.1:8000/metrics",
		"http://localhost:8000/v1/":      "http://localhost:8000/metrics",
		"http://10.10.10.2:8000/v1":      "http://10.10.10.2:8000/metrics",
		"http://192.168.0.5:9000":        "http://192.168.0.5:9000/metrics",
		"http://100.125.220.117:8000/v1": "http://100.125.220.117:8000/metrics", // Tailscale CGNAT
		// Never probe public endpoints.
		"https://api.openai.com/v1":    "",
		"https://openrouter.ai/api/v1": "",
		"http://100.32.0.1:8000/v1":    "", // 100.x outside 100.64/10
		"http://example.internal:8000": "", // unresolved hostname — reject
		"not a url":                    "",
		"":                             "",
	}
	for in, want := range cases {
		if got := engineMetricsURL(in); got != want {
			t.Errorf("engineMetricsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// The operator override must win over derivation (the wormhole cutover left
// derivation pointing at a router with no /metrics), and a misconfigured
// override — public host, garbage URL — must fail safe to "" instead of
// probing an external service.
func TestResolveEngineMetricsURL_OverrideWinsButRejectsUnsafeValues(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "http://100.125.220.117:8000/metrics")
	if got := resolveEngineMetricsURL("http://127.0.0.1:18800/v1"); got != "http://100.125.220.117:8000/metrics" {
		t.Errorf("override not used: %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "https://api.openai.com/metrics")
	if got := resolveEngineMetricsURL("http://127.0.0.1:8000/v1"); got != "" {
		t.Errorf("public override must fail safe, got %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "not a url")
	if got := resolveEngineMetricsURL("http://127.0.0.1:8000/v1"); got != "" {
		t.Errorf("garbage override must fail safe, got %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "")
	if got := resolveEngineMetricsURL("http://127.0.0.1:8000/v1"); got != "http://127.0.0.1:8000/metrics" {
		t.Errorf("derivation broken without override: %q", got)
	}
}

// The override is persisted into run.cache events — credential-bearing URLs
// must fail safe instead of leaking into agent logs.
func TestResolveEngineMetricsURL_OverrideRejectsCredentialParts(t *testing.T) {
	for _, bad := range []string{
		"http://user:pass@100.125.220.117:8000/metrics",
		"http://100.125.220.117:8000/metrics?token=abc",
		"http://100.125.220.117:8000/metrics#frag",
	} {
		t.Setenv(engineMetricsURLEnv, bad)
		if got := resolveEngineMetricsURL("http://100.125.220.117:8000/v1"); got != "" {
			t.Errorf("override %q should fail safe, got %q", bad, got)
		}
	}
}

// The explicit list (DENEB_ENGINE_METRICS_MODELS) keeps its substring
// semantics, so a drop-in that sets it keeps meaning what it meant.
func TestEngineMetricsModelAllowed(t *testing.T) {
	cases := []struct {
		model, filter string
		want          bool
	}{
		{"glm-5.2", "", true},
		{"deepseek-v4-flash", "deepseek,dsv4", true},
		{"dsv4-nothink", "deepseek,dsv4", true},
		{"glm-5.2", "deepseek,dsv4", false},
		{"GLM-5.2", "glm", true},
	}
	for _, c := range cases {
		if got := engineMetricsModelAllowed(c.model, c.filter); got != c.want {
			t.Errorf("allowed(%q,%q) = %v, want %v", c.model, c.filter, got, c.want)
		}
	}
}

// The production router when the engine screen gained its model switch: four
// entries at the engine; glm-5.3, deepseek-v4-flash-api and k3 in the cloud.
var testEngineEntries = []string{"glm-5.3-flash", "glm-5.3-flash-low", "qwen3.8-flash-next", "qwen3.8-flash-next-low"}

const (
	testRouterBaseURL = "http://127.0.0.1:18800/v1"
	testEngineMetrics = "http://100.125.220.117:8000/metrics"
	testEngineBaseURL = "http://100.125.220.117:8000/v1"
)

// A run reached the engine when it was sent straight there, or went through
// the router under the exact name of an entry the router sends there. A cloud
// run never counts: not the router's cloud entry, not a longer or
// vendor-prefixed near-name, and not a cloud twin answering under the local
// entry's own name.
func TestEngineServedRun(t *testing.T) {
	cases := []struct {
		name, baseURL, model string
		want                 bool
	}{
		{"router, engine entry", testRouterBaseURL, "qwen3.8-flash-next", true},
		{"router, engine entry's other variant", testRouterBaseURL, "glm-5.3-flash-low", true},
		{"router, cloud entry", testRouterBaseURL, "glm-5.3", false},
		{"router, longer name than an entry", testRouterBaseURL, "glm-5.3-flash-nothink", false},
		{"cloud twin under the local name", "https://api.z.ai/api/paas/v4", "glm-5.3-flash", false},
		{"vendor-prefixed near-name", "https://openrouter.ai/api/v1", "z-ai/glm-5.3-flash", false},
		{"straight at the engine", testEngineBaseURL, "whatever-it-serves", true},
		{"no base URL", "", "qwen3.8-flash-next", false},
	}
	for _, c := range cases {
		if got := engineServedRun(c.baseURL, c.model, testEngineMetrics, testEngineEntries); got != c.want {
			t.Errorf("%s: engineServedRun(%q, %q) = %v, want %v", c.name, c.baseURL, c.model, got, c.want)
		}
	}
	// Without the router's entries only a run sent straight at the engine counts.
	if engineServedRun(testRouterBaseURL, "qwen3.8-flash-next", testEngineMetrics, nil) {
		t.Error("a router run must not count without the router's entries")
	}
	if !engineServedRun(testEngineBaseURL, "qwen3.8-flash-next", testEngineMetrics, nil) {
		t.Error("a run sent straight at the engine counts without the router's entries")
	}
}

// Without the env list the scope follows the router, so a Qwen run is sampled
// the moment the engine serves Qwen. A set list still wins both ways — the
// drop-ins that set it keep meaning what they meant — but a list that drops a
// model the engine serves is reported, once per model, instead of going blind
// in silence.
func TestEngineCacheSampleScoped(t *testing.T) {
	var asked []string
	engineModels := func(engineURL string) []string {
		asked = append(asked, engineURL)
		return testEngineEntries
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	engineScopeWarned.Delete("qwen3.8-flash-next")
	t.Cleanup(func() { engineScopeWarned.Delete("qwen3.8-flash-next") })

	t.Setenv(engineMetricsModelsEnv, "")
	if !engineCacheSampleScoped(testRouterBaseURL, "qwen3.8-flash-next", testEngineMetrics, engineModels, logger) {
		t.Error("a Qwen run through the router must be sampled when no list is set")
	}
	if engineCacheSampleScoped(testRouterBaseURL, "glm-5.3", testEngineMetrics, engineModels, logger) {
		t.Error("a run on the router's cloud entry must not be sampled")
	}
	if len(asked) == 0 || asked[0] != testEngineMetrics {
		t.Errorf("router entries asked for %q, want the engine's metrics URL", asked)
	}
	if engineCacheSampleScoped(testRouterBaseURL, "qwen3.8-flash-next", testEngineMetrics, nil, logger) {
		t.Error("without the router's entries a router run must not be sampled")
	}

	t.Setenv(engineMetricsModelsEnv, "glm-5.3-flash") // the production drop-in's value
	if !engineCacheSampleScoped(testRouterBaseURL, "glm-5.3-flash-low", testEngineMetrics, engineModels, logger) {
		t.Error("an explicit list must still admit what it names")
	}
	for range 2 {
		if engineCacheSampleScoped(testRouterBaseURL, "qwen3.8-flash-next", testEngineMetrics, engineModels, logger) {
			t.Error("an explicit list must still win over the router")
		}
	}
	if engineCacheSampleScoped(testRouterBaseURL, "glm-5.3", testEngineMetrics, engineModels, logger) {
		t.Error("an explicit list must not admit a model it does not name")
	}
	out := logs.String()
	if n := strings.Count(out, "excludes a model the engine serves"); n != 1 || !strings.Contains(out, "model=qwen3.8-flash-next") {
		t.Errorf("want exactly one warning, for the served model the list drops (glm-5.3 is not the engine's):\n%s", out)
	}
}

// End to end, the regression itself: no list, the engine serving Qwen, and a
// run the router sent there under an entry no hand-kept list named — it logs
// run.cache with the engine's delta.
func TestLogEngineCacheAsync_SamplesARouterRunOnAnEngineEntry(t *testing.T) {
	var mu sync.Mutex
	hits, queries := 1000.0, 2000.0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(w, "vllm:prefix_cache_hits_total %g\n", hits)
		fmt.Fprintf(w, "vllm:prefix_cache_queries_total %g\n", queries)
	}))
	defer srv.Close()
	metricsURL := srv.URL + "/metrics"
	t.Setenv(engineMetricsURLEnv, metricsURL)
	t.Setenv(engineMetricsModelsEnv, "")

	// Baseline synchronously, so the async sample below is a delta.
	if _, _, ok := sampleEngineCacheDelta(context.Background(), metricsURL); ok {
		t.Fatal("baseline sample must not report a delta")
	}
	mu.Lock()
	hits, queries = 1500, 2600
	mu.Unlock()

	w := agentlog.NewWriter(t.TempDir())
	deps := runDeps{engineModels: func(engineURL string) []string {
		if httputil.HostPort(engineURL) == httputil.HostPort(srv.URL) {
			return []string{"qwen3.8-flash-next"}
		}
		return nil
	}}
	client := llm.NewClient(testRouterBaseURL, "", llm.WithAPIMode(llm.APIModeOpenAI))
	logEngineCacheAsync(deps, agentlog.NewRunLogger(w, "client:main", "run-qwen"), client, "qwen3.8-flash-next", false, discardLogger())

	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _ := w.ReadRun("run-qwen")
		for _, e := range entries {
			if e.Type != agentlog.TypeRunCache {
				continue
			}
			var got agentlog.RunCacheData
			if err := json.Unmarshal(e.Data, &got); err != nil {
				t.Fatalf("run.cache data: %v", err)
			}
			if got.EngineHitTokens != 500 || got.EngineQueryTokens != 600 || got.MetricsURL != metricsURL {
				t.Fatalf("run.cache = %+v, want the engine's delta (500, 600) at %s", got, metricsURL)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no run.cache for a router run on an engine entry")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The server hands the router lookup in through HandlerConfig; every run must
// carry it, or router runs quietly fall out of the sample again.
func TestHandlerCarriesEngineModelsIntoRunDeps(t *testing.T) {
	cfg := DefaultHandlerConfig()
	cfg.EngineModels = func(string) []string { return []string{"qwen3.8-flash-next"} }
	h := NewHandler(session.NewManager(), nil, nil, cfg)
	defer h.Close()
	if deps := h.buildRunDeps(); deps.engineModels == nil || !slices.Equal(deps.engineModels(testEngineMetrics), []string{"qwen3.8-flash-next"}) {
		t.Fatal("runDeps dropped HandlerConfig.EngineModels")
	}
}

func TestSampleEngineCacheDeltaReturnsDeltaAfterBaseline(t *testing.T) {
	hits, queries := 1000.0, 2000.0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "# HELP vllm:prefix_cache_hits_total Prefix cache hits\n")
		fmt.Fprintf(w, "vllm:prefix_cache_hits_created{engine=\"0\"} 1.78e+09\n") // shared-prefix trap
		fmt.Fprintf(w, "vllm:prefix_cache_hits_total{engine=\"0\"} %g\n", hits)
		fmt.Fprintf(w, "vllm:prefix_cache_queries_total{engine=\"0\"} %g\n", queries)
	}))
	defer srv.Close()

	ctx := context.Background()
	// First sample establishes the baseline — no delta yet.
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("baseline sample must not report a delta")
	}
	// Counters advance → delta reported.
	hits, queries = 1500.0, 2600.0
	h, q, ok := sampleEngineCacheDelta(ctx, srv.URL)
	if !ok || h != 500 || q != 600 {
		t.Fatalf("expected delta (500, 600), got (%d, %d, ok=%v)", h, q, ok)
	}
	// Engine restart (counters reset) → re-baseline, no delta.
	hits, queries = 10.0, 20.0
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("counter regression must re-baseline, not report a negative delta")
	}
	// And the next advance is a clean delta from the new baseline.
	hits, queries = 110.0, 220.0
	h, q, ok = sampleEngineCacheDelta(ctx, srv.URL)
	if !ok || h != 100 || q != 200 {
		t.Fatalf("expected delta (100, 200) after re-baseline, got (%d, %d, ok=%v)", h, q, ok)
	}
}

func TestSampleEngineCacheDelta_MissingMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "some_other_metric 1\n")
	}))
	defer srv.Close()
	if _, _, ok := sampleEngineCacheDelta(context.Background(), srv.URL); ok {
		t.Fatal("missing counters must not report a delta")
	}
}

func TestMetricValueParsesLabeledAndBareSamples(t *testing.T) {
	if v, ok := metricValue(`vllm:prefix_cache_hits_total{engine="0"} 1.3316096e+07`, metricPrefixCacheHits); !ok || v != 1.3316096e+07 {
		t.Fatalf("labeled sample: got (%v, %v)", v, ok)
	}
	if v, ok := metricValue("vllm:prefix_cache_hits_total 42", metricPrefixCacheHits); !ok || v != 42 {
		t.Fatalf("bare sample: got (%v, %v)", v, ok)
	}
	// A longer metric sharing the prefix must not match (the _created trap).
	if _, ok := metricValue(`vllm:prefix_cache_hits_created{engine="0"} 1.78e+09`, metricPrefixCacheHits); ok {
		t.Fatal("prefix-sharing metric must not match")
	}
}
