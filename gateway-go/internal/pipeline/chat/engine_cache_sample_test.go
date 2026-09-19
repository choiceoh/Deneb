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
// override — public host, garbage URL — must fail safe to nothing instead of
// probing an external service, and without falling back to derivation.
func TestResolveEngineMetricsURLs_OverrideWinsButRejectsUnsafeValues(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "http://100.125.220.117:8000/metrics")
	if got := resolveEngineMetricsURLs("http://127.0.0.1:18800/v1"); !slices.Equal(got, []string{"http://100.125.220.117:8000/metrics"}) {
		t.Errorf("override not used: %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "https://api.openai.com/metrics")
	if got := resolveEngineMetricsURLs("http://127.0.0.1:8000/v1"); len(got) != 0 {
		t.Errorf("public override must fail safe, got %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "not a url")
	if got := resolveEngineMetricsURLs("http://127.0.0.1:8000/v1"); len(got) != 0 {
		t.Errorf("garbage override must fail safe, got %q", got)
	}

	t.Setenv(engineMetricsURLEnv, "")
	if got := resolveEngineMetricsURLs("http://127.0.0.1:8000/v1"); !slices.Equal(got, []string{"http://127.0.0.1:8000/metrics"}) {
		t.Errorf("derivation broken without override: %q", got)
	}
}

// The override is persisted into run.cache events — credential-bearing URLs
// must fail safe instead of leaking into agent logs.
func TestResolveEngineMetricsURLs_OverrideRejectsCredentialParts(t *testing.T) {
	for _, bad := range []string{
		"http://user:pass@100.125.220.117:8000/metrics",
		"http://100.125.220.117:8000/metrics?token=abc",
		"http://100.125.220.117:8000/metrics#frag",
	} {
		t.Setenv(engineMetricsURLEnv, bad)
		if got := resolveEngineMetricsURLs("http://100.125.220.117:8000/v1"); len(got) != 0 {
			t.Errorf("override %q should fail safe, got %q", bad, got)
		}
	}
}

// The variable is a comma-separated list, one entry per engine — the parse
// enginespeed.Endpoints gives the engine-speed task and the liveness watcher.
// Read as one URL, "A,B" is host A with the path "/metrics,http://B…", and
// every scrape of it 404s in silence. Each entry passes or fails the safety
// rule on its own, so an unsafe neighbor costs only itself.
func TestResolveEngineMetricsURLs_ReadsTheListEntryByEntry(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, testEngineMetrics+", "+testQwenEngineMetrics)
	if got := resolveEngineMetricsURLs(testRouterBaseURL); !slices.Equal(got, []string{testEngineMetrics, testQwenEngineMetrics}) {
		t.Errorf("two engines resolved to %q, want both, each whole", got)
	}

	t.Setenv(engineMetricsURLEnv, "https://api.openai.com/metrics,"+testEngineMetrics+",http://user:pass@100.125.220.118:8000/metrics")
	if got := resolveEngineMetricsURLs(testRouterBaseURL); !slices.Equal(got, []string{testEngineMetrics}) {
		t.Errorf("mixed list resolved to %q, want only the safe entry", got)
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

// A fleet that serves two models locally: GLM on the engine above, Qwen on a
// second one. Listed GLM first, so a pick by position would always land there.
const (
	testQwenEngineMetrics = "http://100.125.220.118:8000/metrics"
	testQwenEngineBaseURL = "http://100.125.220.118:8000/v1"
)

var testTwoEngines = []string{testEngineMetrics, testQwenEngineMetrics}

// testTwoEngineModels is the router's table for testTwoEngines, keyed on host
// and port the way configresolve.EngineModels matches.
func testTwoEngineModels(engineURL string) []string {
	switch httputil.HostPort(engineURL) {
	case httputil.HostPort(testEngineMetrics):
		return []string{"glm-5.3-flash", "glm-5.3-flash-low"}
	case httputil.HostPort(testQwenEngineMetrics):
		return []string{"qwen3.8-flash-next", "qwen3.8-flash-next-low"}
	}
	return nil
}

// A run reached the engine when it was sent straight there, or went through
// the router under the exact name of an entry the router sends there. A cloud
// run never counts: not the router's cloud entry, not a longer or
// vendor-prefixed near-name, and not a cloud twin answering under the local
// entry's own name.
func TestServingEngine(t *testing.T) {
	engineModels := func(string) []string { return testEngineEntries }
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
		want := ""
		if c.want {
			want = testEngineMetrics
		}
		route := engineRoute{baseURL: c.baseURL, model: c.model, engineModels: engineModels}
		if got := route.servingEngine([]string{testEngineMetrics}); got != want {
			t.Errorf("%s: servingEngine(%q, %q) = %q, want %q", c.name, c.baseURL, c.model, got, want)
		}
	}
	// Without the router's entries only a run sent straight at the engine counts.
	if got := (engineRoute{baseURL: testRouterBaseURL, model: "qwen3.8-flash-next"}).servingEngine([]string{testEngineMetrics}); got != "" {
		t.Errorf("a router run must not count without the router's entries, got %q", got)
	}
	if got := (engineRoute{baseURL: testEngineBaseURL, model: "qwen3.8-flash-next"}).servingEngine([]string{testEngineMetrics}); got != testEngineMetrics {
		t.Errorf("a run sent straight at the engine counts without the router's entries, got %q", got)
	}
}

// With two engines listed, a run is placed at the engine that served it —
// never at the first one listed for want of a better idea. Its own address
// decides before the router's table does: a role pointed straight at the Qwen
// engine was answered there, whatever the router would do with the name.
func TestServingEngineAmongSeveral(t *testing.T) {
	cases := []struct {
		name, baseURL, model, want string
	}{
		{"router, second engine's entry", testRouterBaseURL, "qwen3.8-flash-next-low", testQwenEngineMetrics},
		{"router, first engine's entry", testRouterBaseURL, "glm-5.3-flash", testEngineMetrics},
		{"straight at the second engine", testQwenEngineBaseURL, "whatever-it-serves", testQwenEngineMetrics},
		{"straight at the second engine, first engine's name", testQwenEngineBaseURL, "glm-5.3-flash", testQwenEngineMetrics},
		{"router, cloud entry", testRouterBaseURL, "glm-5.3", ""},
		{"cloud twin under a local name", "https://api.z.ai/api/paas/v4", "qwen3.8-flash-next", ""},
	}
	for _, c := range cases {
		route := engineRoute{baseURL: c.baseURL, model: c.model, engineModels: testTwoEngineModels}
		if got := route.servingEngine(testTwoEngines); got != c.want {
			t.Errorf("%s: servingEngine = %q, want %q", c.name, got, c.want)
		}
	}
}

// Without the env list the scope follows the router, so a Qwen run is sampled
// the moment the engine serves Qwen. A set list still wins both ways — the
// drop-ins that set it keep meaning what they meant — but a list that drops a
// model the engine serves is reported, once per model, instead of going blind
// in silence.
func TestEngineCacheSampleTarget(t *testing.T) {
	var asked []string
	engineModels := func(engineURL string) []string {
		asked = append(asked, engineURL)
		return testEngineEntries
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	engineScopeWarned.Delete("qwen3.8-flash-next")
	t.Cleanup(func() { engineScopeWarned.Delete("qwen3.8-flash-next") })

	one := []string{testEngineMetrics}
	target := func(model string, engineModels func(string) []string) string {
		return engineCacheSampleTarget(engineRoute{baseURL: testRouterBaseURL, model: model, engineModels: engineModels}, one, logger)
	}

	t.Setenv(engineMetricsModelsEnv, "")
	if got := target("qwen3.8-flash-next", engineModels); got != testEngineMetrics {
		t.Errorf("a Qwen run through the router must be sampled at the engine when no list is set, got %q", got)
	}
	if got := target("glm-5.3", engineModels); got != "" {
		t.Errorf("a run on the router's cloud entry must not be sampled, got %q", got)
	}
	if len(asked) == 0 || asked[0] != testEngineMetrics {
		t.Errorf("router entries asked for %q, want the engine's metrics URL", asked)
	}
	if got := target("qwen3.8-flash-next", nil); got != "" {
		t.Errorf("without the router's entries a router run must not be sampled, got %q", got)
	}

	t.Setenv(engineMetricsModelsEnv, "glm-5.3-flash") // the production drop-in's value
	if got := target("glm-5.3-flash-low", engineModels); got != testEngineMetrics {
		t.Errorf("an explicit list must still admit what it names, got %q", got)
	}
	for range 2 {
		if got := target("qwen3.8-flash-next", engineModels); got != "" {
			t.Errorf("an explicit list must still win over the router, got %q", got)
		}
	}
	if got := target("glm-5.3", engineModels); got != "" {
		t.Errorf("an explicit list must not admit a model it does not name, got %q", got)
	}
	out := logs.String()
	if n := strings.Count(out, "excludes a model the engine serves"); n != 1 || !strings.Contains(out, "model=qwen3.8-flash-next") {
		t.Errorf("want exactly one warning, for the served model the list drops (glm-5.3 is not the engine's):\n%s", out)
	}
}

// With several engines the explicit list still decides WHETHER a run is
// sampled, never WHERE: an admitted run is sampled at the engine that served
// it. One it admits by name while no engine is known to serve it (here: the
// router's config is unreadable) is sampled at a lone engine — what the list
// always meant — but among several it goes unsampled, since a pick would log
// one engine's delta under a run it may not have served; that is said once.
func TestEngineCacheSampleTarget_ExplicitListAmongSeveralEngines(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	engineUnplacedWarned.Delete("glm-5.3-flash")
	t.Cleanup(func() { engineUnplacedWarned.Delete("glm-5.3-flash") })
	t.Setenv(engineMetricsModelsEnv, "glm-5.3-flash,qwen3.8")

	qwenRun := engineRoute{baseURL: testRouterBaseURL, model: "qwen3.8-flash-next", engineModels: testTwoEngineModels}
	if got := engineCacheSampleTarget(qwenRun, testTwoEngines, logger); got != testQwenEngineMetrics {
		t.Errorf("an admitted Qwen run must be sampled at the Qwen engine, got %q", got)
	}

	unreadable := func(string) []string { return nil }
	glmRun := engineRoute{baseURL: testRouterBaseURL, model: "glm-5.3-flash", engineModels: unreadable}
	if got := engineCacheSampleTarget(glmRun, []string{testEngineMetrics}, logger); got != testEngineMetrics {
		t.Errorf("with one engine an admitted run is sampled there, got %q", got)
	}
	for range 2 {
		if got := engineCacheSampleTarget(glmRun, testTwoEngines, logger); got != "" {
			t.Errorf("among several engines an unplaced run must not be sampled, got %q", got)
		}
	}
	out := logs.String()
	if n := strings.Count(out, "admits a model no configured engine is known to serve"); n != 1 || !strings.Contains(out, "model=glm-5.3-flash") {
		t.Errorf("want exactly one warning, for the run no engine could be named for:\n%s", out)
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

	got := waitRunCache(t, w, "run-qwen")
	if got.EngineHitTokens != 500 || got.EngineQueryTokens != 600 || got.MetricsURL != metricsURL {
		t.Fatalf("run.cache = %+v, want the engine's delta (500, 600) at %s", got, metricsURL)
	}
}

// End to end with two engines listed, the one the run did not use first: the
// run the router sent to the Qwen engine logs the Qwen engine's delta, and the
// GLM engine is never scraped for it. With the list read as one URL, this run
// logged nothing at all.
func TestLogEngineCacheAsync_SamplesTheEngineThatServedTheRun(t *testing.T) {
	var mu sync.Mutex
	hits, queries := 1000.0, 2000.0
	glmScrapes := 0
	glm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		glmScrapes++
		fmt.Fprint(w, "vllm:prefix_cache_hits_total 7\nvllm:prefix_cache_queries_total 9\n")
	}))
	defer glm.Close()
	qwen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(w, "vllm:prefix_cache_hits_total %g\n", hits)
		fmt.Fprintf(w, "vllm:prefix_cache_queries_total %g\n", queries)
	}))
	defer qwen.Close()
	glmMetrics, qwenMetrics := glm.URL+"/metrics", qwen.URL+"/metrics"
	t.Setenv(engineMetricsURLEnv, glmMetrics+","+qwenMetrics)
	t.Setenv(engineMetricsModelsEnv, "")

	// Baseline synchronously, so the async sample below is a delta.
	if _, _, ok := sampleEngineCacheDelta(context.Background(), qwenMetrics); ok {
		t.Fatal("baseline sample must not report a delta")
	}
	mu.Lock()
	hits, queries = 1500, 2600
	mu.Unlock()

	w := agentlog.NewWriter(t.TempDir())
	deps := runDeps{engineModels: func(engineURL string) []string {
		switch httputil.HostPort(engineURL) {
		case httputil.HostPort(glm.URL):
			return []string{"glm-5.3-flash"}
		case httputil.HostPort(qwen.URL):
			return []string{"qwen3.8-flash-next"}
		}
		return nil
	}}
	client := llm.NewClient(testRouterBaseURL, "", llm.WithAPIMode(llm.APIModeOpenAI))
	logEngineCacheAsync(deps, agentlog.NewRunLogger(w, "client:main", "run-two-engines"), client, "qwen3.8-flash-next", false, discardLogger())

	got := waitRunCache(t, w, "run-two-engines")
	if got.EngineHitTokens != 500 || got.EngineQueryTokens != 600 || got.MetricsURL != qwenMetrics {
		t.Fatalf("run.cache = %+v, want the Qwen engine's delta (500, 600) at %s", got, qwenMetrics)
	}
	mu.Lock()
	defer mu.Unlock()
	if glmScrapes != 0 {
		t.Errorf("the GLM engine was scraped %d times for a run it did not serve", glmScrapes)
	}
}

// waitRunCache returns the run's run.cache event once the async sampler has
// written it, failing the test after five seconds without one.
func waitRunCache(t *testing.T, w *agentlog.Writer, runID string) agentlog.RunCacheData {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _ := w.ReadRun(runID)
		for _, e := range entries {
			if e.Type != agentlog.TypeRunCache {
				continue
			}
			var got agentlog.RunCacheData
			if err := json.Unmarshal(e.Data, &got); err != nil {
				t.Fatalf("run.cache data: %v", err)
			}
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("no run.cache for run %s", runID)
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
