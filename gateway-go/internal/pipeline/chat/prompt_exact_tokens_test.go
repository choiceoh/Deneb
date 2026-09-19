package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// tokenizeEngine is a fake serving engine whose /tokenize reports count
// tokens for any text.
func tokenizeEngine(t *testing.T, count int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenize" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"count": count, "max_model_len": 1048576})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitExactHead finalizes the prompt until the engine's count has landed — as
// every turn after the first on the same head does — and returns that turn's
// budget outcome.
func waitExactHead(t *testing.T, head json.RawMessage, cfg ContextConfig, route engineRoute) promptBudgetOutcome {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, outcome := finalizePrompt(head, "", "기억 블록", cfg, "", "", route, nil); outcome.BaseTokensExact {
			return outcome
		}
		if time.Now().After(deadline) {
			t.Fatal("the engine's count never reached the budget")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The head is the large half of the budget subtraction and tier-1 memory gets
// the remainder, so an estimated head puts the estimator's whole error band on
// the quantity that decides admission. When the engine can answer exactly, the
// budget must use that answer.
func TestFinalizePromptUsesEngineTokenCountForTheHead(t *testing.T) {
	const exactHeadTokens = 90_000 // far from any estimate of the short head below
	srv := tokenizeEngine(t, exactHeadTokens)
	t.Setenv(engineMetricsURLEnv, srv.URL+"/metrics")

	head := json.RawMessage(`"` + strings.Repeat("시스템 프롬프트 본문. ", 40) + `"`)
	cfg := ContextConfig{SystemPromptBudget: 100_000}

	// First turn: the fill has not landed, so the estimate stands — exactly the
	// behaviour before this capability existed.
	_, first := finalizePrompt(head, "", "기억 블록", cfg, "", "", engineRoute{}, nil)
	if first.BaseTokensExact {
		t.Error("the first turn cannot already know the exact size")
	}

	outcome := waitExactHead(t, head, cfg, engineRoute{})
	if outcome.BaseTokens != exactHeadTokens {
		t.Errorf("BaseTokens = %d, want the engine's %d", outcome.BaseTokens, exactHeadTokens)
	}
	// 90K of head against a 100K budget leaves 10K, and the estimate of this
	// short head would have left nearly all of it: the measurement changes the
	// decision, which is the point.
	if outcome.Tier1AdmittedTokens > 10_000 {
		t.Errorf("admitted %d tokens into a 10K remainder", outcome.Tier1AdmittedTokens)
	}
}

// Two engines serving two models count the same head differently, so the count
// must come from the engine that will read the head: a run the router sends to
// the second engine is counted there, a run sent straight at an engine by that
// engine, and a cloud run — no engine's — by the first engine listed, which is
// what a lone engine has always counted every run with. Read as one URL, the
// two-engine list answered none of them.
func TestExactPromptTokens_CountsWithTheEngineThatServesTheRun(t *testing.T) {
	glm := tokenizeEngine(t, 111)
	qwen := tokenizeEngine(t, 222)
	t.Setenv(engineMetricsURLEnv, glm.URL+"/metrics,"+qwen.URL+"/metrics")
	engineModels := func(engineURL string) []string {
		switch httputil.HostPort(engineURL) {
		case httputil.HostPort(glm.URL):
			return []string{"glm-5.3-flash"}
		case httputil.HostPort(qwen.URL):
			return []string{"qwen3.8-flash-next"}
		}
		return nil
	}
	cases := []struct {
		name  string
		route engineRoute
		want  int
	}{
		{"router, second engine's model", engineRoute{baseURL: testRouterBaseURL, model: "qwen3.8-flash-next", engineModels: engineModels}, 222},
		{"straight at the second engine", engineRoute{baseURL: qwen.URL + "/v1", model: "whatever-it-serves"}, 222},
		{"router, first engine's model", engineRoute{baseURL: testRouterBaseURL, model: "glm-5.3-flash", engineModels: engineModels}, 111},
		{"cloud run", engineRoute{baseURL: "https://api.z.ai/api/paas/v4", model: "glm-5.3", engineModels: engineModels}, 111},
	}
	for _, c := range cases {
		// A head per case, so no case can answer from another's fill.
		text := "시스템 프롬프트 머리 — " + c.name
		deadline := time.Now().Add(3 * time.Second)
		got, ok := exactPromptTokens(text, c.route, nil)
		for !ok && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			got, ok = exactPromptTokens(text, c.route, nil)
		}
		if !ok || got != c.want {
			t.Errorf("%s: counted (%d, %v), want the %d of the engine that serves the run", c.name, got, ok, c.want)
		}
	}
}

// The route finalizePrompt is handed is the one the head is measured with: a
// run sent to the second engine listed gets that engine's count, not the
// first's.
func TestFinalizePromptCountsTheHeadWithTheRunsEngine(t *testing.T) {
	glm := tokenizeEngine(t, 60_000)
	qwen := tokenizeEngine(t, 90_000)
	t.Setenv(engineMetricsURLEnv, glm.URL+"/metrics,"+qwen.URL+"/metrics")

	head := json.RawMessage(`"` + strings.Repeat("큐웬 엔진이 읽을 머리. ", 40) + `"`)
	route := engineRoute{baseURL: qwen.URL + "/v1", model: "qwen3.8-flash-next"}
	outcome := waitExactHead(t, head, ContextConfig{SystemPromptBudget: 100_000}, route)
	if outcome.BaseTokens != 90_000 {
		t.Errorf("BaseTokens = %d, want the 90000 of the engine serving the run", outcome.BaseTokens)
	}
}

// No engine configured is the common case for a cloud-served turn, and it must
// behave exactly as before: estimate, no exactness claimed, no stall.
func TestFinalizePromptFallsBackToTheEstimateWithoutAnEngine(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "")
	_, outcome := finalizePrompt(json.RawMessage(`"짧은 프롬프트"`), "", "기억 블록",
		ContextConfig{SystemPromptBudget: 100_000}, "", "", engineRoute{}, nil)
	if outcome.BaseTokensExact {
		t.Error("no engine can produce no exact count")
	}
	if outcome.BaseTokens <= 0 {
		t.Error("the estimate must still fill BaseTokens")
	}
}

// A public endpoint is refused by the same host guard the other engine probes
// use, so a misconfigured URL degrades to the estimate instead of reaching out
// — in a list too, where the run's own engine may be the unsafe entry.
func TestFinalizePromptRefusesAPublicTokenizeEndpoint(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "https://api.openai.com/v1")
	if _, ok := exactPromptTokens("some head", engineRoute{}, nil); ok {
		t.Error("a public endpoint must never answer")
	}
	t.Setenv(engineMetricsURLEnv, "http://127.0.0.1:9/metrics,https://api.openai.com/v1")
	if _, ok := exactPromptTokens("some head", engineRoute{baseURL: "https://api.openai.com/v1", model: "gpt"}, nil); ok {
		t.Error("a public entry must never answer, even for a run sent straight at it")
	}
}
