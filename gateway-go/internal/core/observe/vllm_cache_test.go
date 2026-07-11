package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseVllmCounter(t *testing.T) {
	const name = "vllm:prefix_cache_queries_total"
	cases := []struct {
		desc      string
		line      string
		wantModel string
		wantValue float64
		wantOK    bool
	}{
		{"labeled", `vllm:prefix_cache_queries_total{engine="0",model_name="deepseek-v4-flash"} 123456`, "deepseek-v4-flash", 123456, true},
		{"scientific notation", `vllm:prefix_cache_queries_total{model_name="m"} 1.2345e+06`, "m", 1.2345e+06, true},
		{"unlabeled", `vllm:prefix_cache_queries_total 42`, "", 42, true},
		{"trailing timestamp", `vllm:prefix_cache_queries_total{model_name="m"} 7 1712345678`, "m", 7, true},
		{"comment", `# TYPE vllm:prefix_cache_queries_total counter`, "", 0, false},
		{"longer name sharing prefix", `vllm:prefix_cache_queries_total_created{model_name="m"} 1.0`, "", 0, false},
		{"different metric", `vllm:prefix_cache_hits_total{model_name="m"} 1`, "", 0, false},
		{"missing value", `vllm:prefix_cache_queries_total{model_name="m"}`, "", 0, false},
		{"garbage value", `vllm:prefix_cache_queries_total{model_name="m"} abc`, "", 0, false},
		{"name only", `vllm:prefix_cache_queries_total`, "", 0, false},
	}
	for _, tc := range cases {
		model, v, ok := parseVllmCounter(tc.line, name)
		if ok != tc.wantOK || model != tc.wantModel || v != tc.wantValue {
			t.Errorf("%s: parseVllmCounter(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tc.desc, tc.line, model, v, ok, tc.wantModel, tc.wantValue, tc.wantOK)
		}
	}
}

func TestPromLabel(t *testing.T) {
	body := `engine="0",engine_model_name="decoy",model_name="real-model"`
	if got := promLabel(body, "model_name"); got != "real-model" {
		t.Errorf("promLabel boundary anchoring failed: got %q, want %q", got, "real-model")
	}
	if got := promLabel(`a="x"`, "model_name"); got != "" {
		t.Errorf("promLabel on absent key: got %q, want empty", got)
	}
}

// The fetch path strips /v1, sums multi-engine shards per model, sorts models,
// and rounds the hit rate to one decimal.
func TestFetchVllmPrefixCaches(t *testing.T) {
	metrics := strings.Join([]string{
		`# HELP vllm:prefix_cache_queries_total Prefix cache queries`,
		`# TYPE vllm:prefix_cache_queries_total counter`,
		`vllm:prefix_cache_queries_total{engine="0",model_name="qwen"} 600`,
		`vllm:prefix_cache_queries_total{engine="1",model_name="qwen"} 400`,
		`vllm:prefix_cache_hits_total{engine="0",model_name="qwen"} 500`,
		`vllm:prefix_cache_hits_total{engine="1",model_name="qwen"} 320`,
		`vllm:prefix_cache_queries_total{model_name="aeon"} 3`,
		`vllm:prefix_cache_hits_total{model_name="aeon"} 1`,
		`vllm:num_requests_running{model_name="qwen"} 2`, // noise — ignored
	}, "\n")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(metrics))
	}))
	defer srv.Close()

	stats := FetchVllmPrefixCaches(context.Background(), []string{srv.URL + "/v1"})
	if gotPath != "/metrics" {
		t.Fatalf("scrape hit %q, want /metrics (the /v1 suffix must be stripped)", gotPath)
	}
	if len(stats) != 2 {
		t.Fatalf("got %d stats, want 2: %+v", len(stats), stats)
	}
	// Sorted by model name: aeon before qwen.
	if stats[0].Model != "aeon" || stats[0].Queries != 3 || stats[0].Hits != 1 || stats[0].HitRatePct != 33.3 {
		t.Errorf("aeon row wrong: %+v", stats[0])
	}
	if stats[1].Model != "qwen" || stats[1].Queries != 1000 || stats[1].Hits != 820 || stats[1].HitRatePct != 82.0 {
		t.Errorf("qwen row wrong (multi-engine sums): %+v", stats[1])
	}
}

// Down, non-vLLM, and empty-input endpoints all degrade to nothing — never an
// error surfaced to the observe caller.
func TestFetchVllmPrefixCaches_Graceful(t *testing.T) {
	if got := FetchVllmPrefixCaches(context.Background(), nil); got != nil {
		t.Errorf("nil input: got %+v, want nil", got)
	}

	notVllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notVllm.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close() // connection refused

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("vllm:prefix_cache_queries_total{model_name=\"m\"} 10\nvllm:prefix_cache_hits_total{model_name=\"m\"} 5\n"))
	}))
	defer live.Close()

	stats := FetchVllmPrefixCaches(context.Background(),
		[]string{dead.URL + "/v1", notVllm.URL + "/v1", live.URL + "/v1"})
	if len(stats) != 1 || stats[0].Model != "m" || stats[0].HitRatePct != 50.0 {
		t.Errorf("only the live endpoint should contribute: %+v", stats)
	}
}
