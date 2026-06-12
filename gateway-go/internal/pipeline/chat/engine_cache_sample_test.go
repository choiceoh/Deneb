package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEngineMetricsURL(t *testing.T) {
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

func TestSampleEngineCacheDelta(t *testing.T) {
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

func TestMetricValue(t *testing.T) {
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
