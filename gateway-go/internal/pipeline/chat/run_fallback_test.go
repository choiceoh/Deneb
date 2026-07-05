package chat

// Direct tests for runAgentWithFallback's failure-handling contracts. These
// paths had no dedicated coverage despite being the highest-risk logic in the
// run pipeline (stall degrade, compaction anti-thrash, circuit gating) — the
// prerequisite safety net for decomposing run_fallback.go.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

func runFallbackForTest(
	t *testing.T,
	server *httptest.Server,
	cfg agent.AgentConfig,
	deps runDeps,
	messages []llm.Message,
) (*agent.AgentResult, string, bool, error) {
	t.Helper()
	logger := discardLogger()
	if deps.logger == nil {
		deps.logger = logger
	}
	client := llm.NewClient(server.URL, "test-key")
	runLog := agentlog.NewRunLogger(nil, "test-session", "test-run")
	return runAgentWithFallback(
		context.Background(), cfg, messages, client, deps,
		"vllm", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, runLog,
	)
}

// A stalled model (per-run timeout fires before any output) with no fallback
// registry must degrade to the ORIGINAL empty timeout result — no error, no
// fallback — preserving the pre-fallback "stall = empty reply" contract that
// run.go's empty-response handling depends on.
func TestRunAgentWithFallback_StallDegradesToEmptyTimeoutResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never answer within cfg.Timeout: the stream stalls until the
		// client-side deadline cancels the request. The absolute bound keeps
		// server.Close from hanging the suite if cancellation never arrives.
		select {
		case <-r.Context().Done():
		case <-time.After(1 * time.Second):
		}
	}))
	defer server.Close()

	cfg := agent.AgentConfig{
		Model:     "stall-model",
		MaxTurns:  2,
		Timeout:   150 * time.Millisecond,
		MaxTokens: 128,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}

	result, actualModel, fellBack, err := runFallbackForTest(t, server, cfg, runDeps{}, messages)
	if err != nil {
		t.Fatalf("err = %v, want nil (stall must degrade, not error)", err)
	}
	if fellBack {
		t.Error("fellBack = true, want false (no registry, no fallback)")
	}
	if actualModel != "stall-model" {
		t.Errorf("actualModel = %q, want %q", actualModel, "stall-model")
	}
	if result == nil {
		t.Fatal("result = nil, want the stalled timeout result")
	}
	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "timeout")
	}
	if strings.TrimSpace(result.AllText) != "" {
		t.Errorf("AllText = %q, want empty (stall produced no output)", result.AllText)
	}
}

// A context-overflow error whose protected head+tail zones already exceed the
// budget must short-circuit to compression_stuck WITHOUT retrying the provider
// (compaction cannot help by construction), and must surface the
// chat.compaction_stuck broadcast for the operator/UI.
func TestRunAgentWithFallback_CompactionStuckOnProtectedZoneOverBudget(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 8192 tokens","code":"context_length_exceeded"}}`))
	}))
	defer server.Close()

	bc := &broadcastCollector{}
	deps := runDeps{
		broadcast: bc.broadcast,
		// Tiny effective budget (MemoryTokenBudget - SystemPromptBudget = 8
		// tokens): the single protected user message below already exceeds
		// it, so guard A must fire on the first compaction attempt.
		contextCfg: ContextConfig{MemoryTokenBudget: 8, SystemPromptBudget: 0},
	}
	cfg := agent.AgentConfig{
		Model:     "overflow-model",
		MaxTurns:  2,
		Timeout:   5 * time.Second,
		MaxTokens: 128,
	}
	messages := []llm.Message{llm.NewTextMessage("user", strings.Repeat("컨텍스트 초과 유발용 장문 메시지. ", 40))}

	result, actualModel, fellBack, err := runFallbackForTest(t, server, cfg, deps, messages)
	if err != nil {
		t.Fatalf("err = %v, want nil (stuck is a graceful result, not an error)", err)
	}
	if fellBack {
		t.Error("fellBack = true, want false")
	}
	if actualModel != "overflow-model" {
		t.Errorf("actualModel = %q, want %q", actualModel, "overflow-model")
	}
	if result == nil || result.StopReason != stopReasonCompressionStuck {
		t.Fatalf("result = %+v, want StopReason %q", result, stopReasonCompressionStuck)
	}
	if requests != 1 {
		t.Errorf("provider requests = %d, want 1 (guard must not re-hit the provider)", requests)
	}
	found := false
	for _, ev := range bc.get() {
		if ev.Event == "chat.compaction_stuck" {
			found = true
			p, ok := ev.Payload.(ChatCompactionStuckEvent)
			if !ok {
				t.Fatalf("payload type = %T, want ChatCompactionStuckEvent", ev.Payload)
			}
			if p.Reason != "protected_zone_exceeds_budget" {
				t.Errorf("stuck reason = %q, want protected_zone_exceeds_budget", p.Reason)
			}
		}
	}
	if !found {
		t.Error("missing chat.compaction_stuck broadcast")
	}
}

// A hard non-transient, non-overflow provider error with no fallback registry
// must surface as an error (nil result) — the caller's handleRunError path.
func TestRunAgentWithFallback_HardErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid request: unknown field"}}`))
	}))
	defer server.Close()

	cfg := agent.AgentConfig{
		Model:     "bad-model",
		MaxTurns:  2,
		Timeout:   5 * time.Second,
		MaxTokens: 128,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}

	result, _, fellBack, err := runFallbackForTest(t, server, cfg, runDeps{}, messages)
	if err == nil {
		t.Fatalf("err = nil, want provider error surfaced (result=%+v)", result)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil on hard error", result)
	}
	if fellBack {
		t.Error("fellBack = true, want false")
	}
}

// healthyFallbackExists gates the circuit-breaker initial-skip: skipping the
// requested model is only allowed when the chain offers a DISTINCT model whose
// breaker is closed — otherwise trying the requested model is still the best move.
func TestHealthyFallbackExists(t *testing.T) {
	t.Run("distinct healthy fallback exists", func(t *testing.T) {
		reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
			MainModel:        "zai/m-main",
			LightweightModel: "zai/m-light",
			FallbackModel:    "zai/m-fb",
		})
		if !healthyFallbackExists(reg, modelrole.RoleMain, "m-main") {
			t.Error("want true: chain offers distinct healthy models (m-light, m-fb)")
		}
	})

	t.Run("all candidates same as failed model", func(t *testing.T) {
		reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
			MainModel:        "zai/m-same",
			LightweightModel: "zai/m-same",
			FallbackModel:    "zai/m-same",
		})
		if healthyFallbackExists(reg, modelrole.RoleMain, "m-same") {
			t.Error("want false: every candidate is the failed model itself")
		}
	})

	t.Run("all candidates unhealthy", func(t *testing.T) {
		reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
			MainModel:        "zai/m-main",
			LightweightModel: "zai/m-light",
			FallbackModel:    "zai/m-fb",
		})
		// Open the breaker on both fallback candidates (3 consecutive failures
		// inside the cooldown window — modelrole/health.go unhealthyStreak).
		for range 3 {
			reg.RecordModelFailure("m-light")
			reg.RecordModelFailure("m-fb")
		}
		if healthyFallbackExists(reg, modelrole.RoleMain, "m-main") {
			t.Error("want false: every distinct candidate's breaker is open")
		}
		// A success on one candidate closes its breaker and re-enables the skip.
		reg.RecordModelSuccess("m-fb")
		if !healthyFallbackExists(reg, modelrole.RoleMain, "m-main") {
			t.Error("want true after m-fb breaker closed")
		}
	})
}
