package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

func TestModelAttributes(t *testing.T) {
	vision := false
	reg := capTestRegistry(t, map[string]modelrole.ProviderResolved{
		"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 131072, Vision: &vision},
	})
	reg.SetTunedMaxTokens("custom-model", 16384)

	attrs := strings.Join(modelAttributes(reg, "acme", "custom-model"), " · ")
	for _, want := range []string{"컨텍스트 128K", "vision 비활성", "출력 floor 16384"} {
		if !strings.Contains(attrs, want) {
			t.Errorf("attrs %q missing %q", attrs, want)
		}
	}

	// Qwen builtin profile surfaces temp + reasoning channel.
	attrs = strings.Join(modelAttributes(reg, "vllm", "qwen3.6-35b"), " · ")
	for _, want := range []string{"reasoning 채널", "temp 0.7"} {
		if !strings.Contains(attrs, want) {
			t.Errorf("qwen attrs %q missing %q", attrs, want)
		}
	}

	// A fully-unknown model shows nothing rather than noise.
	if got := modelAttributes(reg, "zai", "glm-5-turbo"); len(got) != 0 {
		t.Errorf("permissive model attrs = %v, want empty", got)
	}
}

func TestAppendScorecardSummary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)

	// No scorecard yet → silent.
	var b strings.Builder
	appendScorecardSummary(&b, "m1")
	if b.Len() != 0 {
		t.Fatalf("missing scorecard must add nothing, got %q", b.String())
	}

	// Write a scorecard with one model + one recommendation.
	raw, _ := json.Marshal(map[string]any{
		"generatedAtMs": 1000,
		"windowHours":   24,
		"models": []map[string]any{{
			"model": "m1", "provider": "p", "runs": 12, "p95Ms": 8000,
			"cacheReadTokens": 9000, "cacheCreationTokens": 100, "inputTokens": 1000,
			"fallbackRuns": 2, "timeoutRuns": 1,
		}},
		"recommendations": []map[string]any{{
			"model": "m1", "provider": "p", "rule": "stall", "message": "스톨 점검",
		}},
	})
	if err := os.WriteFile(filepath.Join(dir, "model-stats.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	b.Reset()
	appendScorecardSummary(&b, "m1")
	out := b.String()
	for _, want := range []string{"최근 24h", "12런", "p95 8초", "캐시 히트 90%", "폴백 2회", "스톨 1회", "스톨 점검"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary %q missing %q", out, want)
		}
	}

	// Different model → stats omitted.
	b.Reset()
	appendScorecardSummary(&b, "other")
	if strings.Contains(b.String(), "최근 24h") {
		t.Errorf("other model must not show m1 stats: %q", b.String())
	}
}
