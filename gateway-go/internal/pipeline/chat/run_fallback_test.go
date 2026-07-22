package chat

// Direct tests for runAgentWithFallback's failure-handling contracts. These
// paths had no dedicated coverage despite being the highest-risk logic in the
// run pipeline (stall degrade, compaction anti-thrash, circuit gating) — the
// prerequisite safety net for decomposing run_fallback.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
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
		case <-time.After(300 * time.Millisecond):
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
			var p ChatCompactionStuckEvent
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("payload unmarshal = %v, want ChatCompactionStuckEvent", err)
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

// TestIsEmptyFinalResult pins the accidental-empty-completion classifier: an
// end_turn with tool activity (Turns > 1) and zero text is a failure surface
// (blank bubble), while intentional silence (NO_REPLY token), single-shot
// answers, and timeout stalls (isStalledResult's territory) are left alone.
func TestIsEmptyFinalResult(t *testing.T) {
	cases := []struct {
		name string
		r    *agent.AgentResult
		want bool
	}{
		{"nil result", nil, false},
		{"empty after tool round", &agent.AgentResult{StopReason: "end_turn", Turns: 2}, true},
		{"whitespace only after tools", &agent.AgentResult{StopReason: "end_turn", Turns: 3, AllText: " \n\t"}, true},
		{"NO_REPLY intentional silence", &agent.AgentResult{StopReason: "end_turn", Turns: 2, AllText: "NO_REPLY"}, false},
		{"text produced", &agent.AgentResult{StopReason: "end_turn", Turns: 2, AllText: "답변"}, false},
		{"single-shot empty left alone", &agent.AgentResult{StopReason: "end_turn", Turns: 1}, false},
		{"timeout is the stall path", &agent.AgentResult{StopReason: "timeout", Turns: 2}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyFinalResult(tc.r); got != tc.want {
				t.Errorf("isEmptyFinalResult() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The "model failed, trying fallback" log must blame the role that actually
// failed, never chain[i-1] — that can be a rung skipped without an attempt
// (unassigned model / no client). Observed live 2026-07-17: main (kimi)
// failed while main2 was dormant (unset), yet every walk log line said
// failedRole=main2, muddying the diagnosis.
func TestRunAgentWithFallback_FailedRoleLogSkipsUnassignedRungs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel: "test/m-main",
		// Main2Model deliberately unset: the main chain's second rung is
		// skipped without an attempt — the shape that used to get blamed.
		CodingModel:      "test/m-coding",
		LightweightModel: "test/m-light",
		FallbackModel:    "test/m-fb",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := agent.AgentConfig{
		Model:     "m-main",
		MaxTurns:  2,
		Timeout:   5 * time.Second,
		MaxTokens: 128,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}
	client := llm.NewClient(server.URL, "test-key")
	runLog := agentlog.NewRunLogger(nil, "test-session", "test-run")

	_, _, _, err := runAgentWithFallback(
		context.Background(), cfg, messages, client,
		runDeps{registry: reg, logger: logger},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, runLog,
	)
	if err == nil {
		t.Fatal("want error: every model in the chain 400s")
	}

	logs := buf.String()
	if strings.Contains(logs, "failedRole=main2") {
		t.Errorf("skipped rung blamed for the failure:\n%s", logs)
	}
	if !strings.Contains(logs, "failedRole=main nextRole=coding") {
		t.Errorf("first walk line should blame main and target coding:\n%s", logs)
	}
	if !strings.Contains(logs, "failedRole=coding nextRole=lightweight") {
		t.Errorf("second walk line should advance the blame to coding:\n%s", logs)
	}
}

// resultRanSideEffectingTool must treat only the read-only allowlist as
// replay-safe; any mutating, action-multiplexed, or unknown tool (or a tool
// count with no histogram) is side-effecting, so a whole-turn replay never
// silently repeats a mutation.
func TestResultRanSideEffectingTool(t *testing.T) {
	cases := []struct {
		name string
		res  *agent.AgentResult
		want bool
	}{
		{"nil result", nil, false},
		{"no tools", &agent.AgentResult{TotalToolCalls: 0}, false},
		{"read-only only", &agent.AgentResult{TotalToolCalls: 3, ToolCounts: map[string]int{"web": 1, "read": 1, "mail_archive": 1}}, false},
		{"a mutating tool ran", &agent.AgentResult{TotalToolCalls: 2, ToolCounts: map[string]int{"web": 1, "exec": 1}}, true},
		{"unknown tool ran", &agent.AgentResult{TotalToolCalls: 1, ToolCounts: map[string]int{"some_new_tool": 1}}, true},
		{"action-multiplexed tool (wiki)", &agent.AgentResult{TotalToolCalls: 1, ToolCounts: map[string]int{"wiki": 1}}, true},
		{"count without histogram assumes side-effecting", &agent.AgentResult{TotalToolCalls: 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resultRanSideEffectingTool(c.res); got != c.want {
				t.Errorf("resultRanSideEffectingTool = %v, want %v", got, c.want)
			}
		})
	}
}

// A transient error AFTER a side-effecting tool committed must not trigger the
// single-shot replay: re-running the turn would execute the mutation again.
func TestRetryTransientSkipsReplayAfterSideEffectingTool(t *testing.T) {
	transient := &httpretry.APIError{StatusCode: 503, Message: "overloaded"}
	if !isTransientLLMError(transient) {
		t.Fatal("precondition: a 503 must classify as transient, else the guard under test is never reached")
	}
	orig := &agent.AgentResult{StopReason: "end_turn", TotalToolCalls: 1, ToolCounts: map[string]int{"exec": 1}}
	// client left nil on purpose: if the guard failed to fire, the replay's
	// agent.RunAgent(nil client) would panic — a clean return proves the skip.
	tr := &fallbackTurn{logger: discardLogger(), runErr: transient, agentResult: orig}

	aborted, err := tr.retryTransient(context.Background())
	if aborted || err != nil {
		t.Fatalf("retryTransient = (%v, %v), want (false, nil)", aborted, err)
	}
	if tr.agentResult != orig {
		t.Error("agentResult changed — the turn was replayed despite a committed side-effecting tool")
	}
	if !errors.Is(tr.runErr, transient) {
		t.Error("runErr changed — the turn was replayed")
	}
}

// The model fallback chain must also skip when a side-effecting tool committed,
// so a mutation is never duplicated on another model.
func TestWalkFallbackChainSkipsAfterSideEffectingTool(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:     "zai/m-main",
		FallbackModel: "zai/m-fb",
	})
	orig := &agent.AgentResult{StopReason: "end_turn", TotalToolCalls: 1, ToolCounts: map[string]int{"exec": 1}}
	tr := &fallbackTurn{
		logger:      discardLogger(),
		deps:        runDeps{registry: reg},
		cfg:         agent.AgentConfig{Model: "m-main"},
		runErr:      &httpretry.APIError{StatusCode: 503},
		agentResult: orig,
	}

	tr.walkFallbackChain(context.Background())
	if tr.fellBack {
		t.Error("fellBack = true — fallback ran despite a committed side-effecting tool")
	}
	if tr.agentResult != orig {
		t.Error("agentResult changed — the fallback chain replayed the turn")
	}
}
