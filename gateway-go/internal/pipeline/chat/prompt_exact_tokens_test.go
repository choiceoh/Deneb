package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The head is the large half of the budget subtraction and tier-1 memory gets
// the remainder, so an estimated head puts the estimator's whole error band on
// the quantity that decides admission. When the engine can answer exactly, the
// budget must use that answer.
func TestFinalizePromptUsesEngineTokenCountForTheHead(t *testing.T) {
	const exactHeadTokens = 90_000 // far from any estimate of the short head below
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenize" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"count": exactHeadTokens, "max_model_len": 1048576})
	}))
	defer srv.Close()
	t.Setenv(engineMetricsURLEnv, srv.URL+"/metrics")

	head := json.RawMessage(`"` + strings.Repeat("시스템 프롬프트 본문. ", 40) + `"`)
	cfg := ContextConfig{SystemPromptBudget: 100_000}

	// First turn: the fill has not landed, so the estimate stands — exactly the
	// behaviour before this capability existed.
	_, first := finalizePrompt(head, "", "기억 블록", cfg, "", "", nil)
	if first.BaseTokensExact {
		t.Error("the first turn cannot already know the exact size")
	}

	var outcome promptBudgetOutcome
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, outcome = finalizePrompt(head, "", "기억 블록", cfg, "", "", nil)
		if outcome.BaseTokensExact {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !outcome.BaseTokensExact {
		t.Fatal("the engine's count never reached the budget")
	}
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

// No engine configured is the common case for a cloud-served turn, and it must
// behave exactly as before: estimate, no exactness claimed, no stall.
func TestFinalizePromptFallsBackToTheEstimateWithoutAnEngine(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "")
	_, outcome := finalizePrompt(json.RawMessage(`"짧은 프롬프트"`), "", "기억 블록",
		ContextConfig{SystemPromptBudget: 100_000}, "", "", nil)
	if outcome.BaseTokensExact {
		t.Error("no engine can produce no exact count")
	}
	if outcome.BaseTokens <= 0 {
		t.Error("the estimate must still fill BaseTokens")
	}
}

// A public endpoint is refused by the same host guard the other engine probes
// use, so a misconfigured URL degrades to the estimate instead of reaching out.
func TestFinalizePromptRefusesAPublicTokenizeEndpoint(t *testing.T) {
	t.Setenv(engineMetricsURLEnv, "https://api.openai.com/v1")
	if _, ok := exactPromptTokens("some head", nil); ok {
		t.Error("a public endpoint must never answer")
	}
}
