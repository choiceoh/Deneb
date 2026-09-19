package observe

import "testing"

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

func TestPromLabelReturnsAnchoredValueIgnoringDecoyPrefix(t *testing.T) {
	body := `engine="0",engine_model_name="decoy",model_name="real-model"`
	if got := promLabel(body, "model_name"); got != "real-model" {
		t.Errorf("promLabel boundary anchoring failed: got %q, want %q", got, "real-model")
	}
	if got := promLabel(`a="x"`, "model_name"); got != "" {
		t.Errorf("promLabel on absent key: got %q, want empty", got)
	}
}
