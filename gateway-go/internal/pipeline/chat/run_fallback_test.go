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
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestRunAgentWithFallback_PreOutputIdleFallsBackWithoutSameModelRetry(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if req.Model == "m-main" {
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
				t.Error("main stall request was not canceled promptly")
			}
			return
		}
		fmt.Fprint(w, sseResponse("fallback reply", "end_turn"))
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:   "test/m-main",
		CodingModel: "test/m-fb",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	cfg := agent.AgentConfig{
		Model:             "m-main",
		MaxTurns:          2,
		Timeout:           5 * time.Second,
		MaxTokens:         128,
		StreamIdleTimeout: 20 * time.Millisecond,
	}
	messages := []llm.Message{llm.NewTextMessage("user", "hello")}
	client := llm.NewClient(server.URL, "test-key")

	start := time.Now()
	result, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, messages, client,
		runDeps{registry: reg, logger: discardLogger()},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, discardLogger(), agentlog.NewRunLogger(nil, "test-session", "test-run"),
	)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("err = %v, want fallback success", err)
	}
	if result == nil || result.Text != "fallback reply" {
		t.Fatalf("result = %+v, want fallback reply", result)
	}
	if actualModel != "m-fb" || !fellBack {
		t.Fatalf("actualModel=%q fellBack=%v, want m-fb true", actualModel, fellBack)
	}
	if elapsed > time.Second {
		t.Fatalf("fallback took %s, want pre-output idle to skip the same-model retry", elapsed)
	}
	mu.Lock()
	gotModels := append([]string(nil), models...)
	mu.Unlock()
	if strings.Join(gotModels, ",") != "m-main,m-fb" {
		t.Fatalf("models called = %v, want exactly main then fallback", gotModels)
	}
}

// A run that exhausts its wall-time budget after committing a tool must not
// replay that mutation from the original user message. It can still recover a
// final answer by handing the completed message journal to a no-tools fallback.
func TestRunAgentWithFallback_MidWorkTimeoutResumesNoToolsFallbackFromCheckpoint(t *testing.T) {
	var (
		mu            sync.Mutex
		mainRequests  int
		fallbackCalls int
		toolCalls     int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model      string            `json:"model"`
			Messages   []json.RawMessage `json:"messages"`
			Tools      []json.RawMessage `json:"tools"`
			ToolChoice json.RawMessage   `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		if req.Model == "m-main" {
			mainRequests++
			requestNumber := mainRequests
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if requestNumber == 1 {
				fmt.Fprint(w, sseToolResponse("tool-1", "exec", `{}`))
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
			return
		}
		fallbackCalls++
		mu.Unlock()

		if len(req.Tools) != 0 {
			t.Errorf("fallback tools = %d, want 0", len(req.Tools))
		}
		if string(req.ToolChoice) != `"none"` {
			t.Errorf("fallback tool_choice = %s, want none", req.ToolChoice)
		}
		checkpointFound := false
		for _, message := range req.Messages {
			checkpointFound = checkpointFound || bytes.Contains(message, []byte("committed-once"))
		}
		if !checkpointFound {
			t.Error("fallback messages do not contain the completed tool checkpoint")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse("checkpoint recovered", "end_turn"))
	}))
	defer server.Close()

	tools := NewToolRegistry()
	tools.Register("exec", func(_ context.Context, _ json.RawMessage) (string, error) {
		mu.Lock()
		toolCalls++
		mu.Unlock()
		return "committed-once", nil
	})
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:     "test/m-main",
		FallbackModel: "test/m-fb",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	cfg := agent.AgentConfig{
		Model:     "m-main",
		MaxTurns:  4,
		Timeout:   5 * time.Second,
		MaxTokens: 128,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	result, actualModel, fellBack, err := runAgentWithFallback(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "do it once")},
		llm.NewClient(server.URL, "test-key"),
		runDeps{registry: reg, tools: tools, logger: discardLogger()},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, discardLogger(),
		agentlog.NewRunLogger(nil, "test-session", "test-run"),
	)
	if err != nil {
		t.Fatalf("err = %v, want checkpoint fallback success", err)
	}
	if result == nil || result.Text != "checkpoint recovered" {
		t.Fatalf("result = %+v, want checkpoint recovery reply", result)
	}
	if actualModel != "m-fb" || !fellBack {
		t.Fatalf("actualModel=%q fellBack=%v, want m-fb true", actualModel, fellBack)
	}
	mu.Lock()
	defer mu.Unlock()
	if mainRequests != 2 || fallbackCalls != 1 {
		t.Errorf("requests main=%d fallback=%d, want 2 and 1", mainRequests, fallbackCalls)
	}
	if toolCalls != 1 {
		t.Errorf("tool calls = %d, want 1 (fallback must not replay the mutation)", toolCalls)
	}
	if result.TotalToolCalls != 1 || result.ToolCounts["exec"] != 1 {
		t.Errorf("merged tool accounting = total %d counts %v, want original exec call", result.TotalToolCalls, result.ToolCounts)
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

func TestRunAgentWithFallback_OpenCircuitSkipIsDebugOnly(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()
		if req.Model == "m-main" {
			t.Error("open breaker should skip the unhealthy main model")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse("fallback reply", "end_turn"))
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-main",
		LightweightModel: "test/m-fb",
		FallbackModel:    "test/m-fb",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	for range 3 {
		reg.RecordModelFailure("m-main")
	}

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

	result, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, messages, client,
		runDeps{registry: reg, logger: logger},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, runLog,
	)
	if err != nil {
		t.Fatalf("err = %v, want fallback success", err)
	}
	if result == nil || result.Text != "fallback reply" {
		t.Fatalf("result = %+v, want fallback reply", result)
	}
	if actualModel != "m-fb" || !fellBack {
		t.Fatalf("actualModel=%q fellBack=%v, want m-fb true", actualModel, fellBack)
	}
	mu.Lock()
	gotModels := append([]string(nil), models...)
	mu.Unlock()
	if strings.Join(gotModels, ",") != "m-fb" {
		t.Fatalf("models called = %v, want only fallback model", gotModels)
	}

	logs := buf.String()
	if strings.Contains(logs, "model circuit open; skipping straight to fallback chain") {
		t.Errorf("open-circuit skip should not be emitted at the default log level:\n%s", logs)
	}
	if strings.Contains(logs, `level=WARN msg="model failed, trying fallback"`) {
		t.Errorf("synthetic circuit-open recovery should not be logged as a model failure:\n%s", logs)
	}
}

// TestIsEmptyFinalResult pins the accidental-empty-completion classifier: an
// end_turn with zero text is a failure surface (blank bubble) whether or not
// tools ran, while intentional silence (NO_REPLY token) and timeout stalls
// (isStalledResult's territory) are left alone.
//
// The single-shot case used to be excluded here, and this test contracted the
// exclusion. It was never justified — Turns > 1 exists to detect tool
// activity, which is what forbids an auto-rerun, not to decide who deserves a
// blank bubble — and it left an empty "end_turn" at Turns 1 uncovered by this
// guard AND by isStalledResult, which only fires on "timeout".
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
		{"single-shot empty is the same blank bubble", &agent.AgentResult{StopReason: "end_turn", Turns: 1}, true},
		{"single-shot NO_REPLY stays intentional", &agent.AgentResult{StopReason: "end_turn", Turns: 1, AllText: "NO_REPLY"}, false},
		{"no round ran at all", &agent.AgentResult{StopReason: "end_turn", Turns: 0}, false},
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
	if strings.Contains(logs, `level=ERROR msg="fallback also failed"`) {
		t.Errorf("fallback rung failure logged at error level; final run failure is surfaced by handleRunError:\n%s", logs)
	}
	if !strings.Contains(logs, `level=WARN msg="fallback also failed"`) {
		t.Errorf("fallback rung failure should remain visible as warn:\n%s", logs)
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
		{"action-multiplexed tool (knowledge)", &agent.AgentResult{TotalToolCalls: 1, ToolCounts: map[string]int{"knowledge": 1}}, true},
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

// A thinking-signature rejection AFTER a side-effecting tool committed must
// not trigger the strip-and-replay retry.
func TestRetryThinkingStripSkipsReplayAfterSideEffectingTool(t *testing.T) {
	sigErr := &httpretry.APIError{
		StatusCode: 400,
		Message:    `{"error":{"message":"messages.1.content.0.thinking.signature: invalid signature for thinking block"}}`,
	}
	if !shouldStripThinking(sigErr) {
		t.Fatal("precondition: thinking signature error must classify for strip retry")
	}
	raw, err := json.Marshal([]llm.ContentBlock{
		{Type: "thinking", Thinking: "reasoning", Signature: "sig"},
		{Type: "text", Text: "answer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	orig := &agent.AgentResult{StopReason: "end_turn", TotalToolCalls: 1, ToolCounts: map[string]int{"exec": 1}}
	tr := &fallbackTurn{
		logger:      discardLogger(),
		runErr:      sigErr,
		agentResult: orig,
		messages: []llm.Message{
			llm.NewTextMessage("user", "do it"),
			{Role: "assistant", Content: llm.FlexibleFromRaw(raw)},
		},
	}
	tr.retryThinkingStrip(context.Background())
	if tr.agentResult != orig {
		t.Error("agentResult changed — thinking-strip replay ran despite a committed side-effecting tool")
	}
	if !errors.Is(tr.runErr, sigErr) {
		t.Error("runErr changed — thinking-strip replay ran")
	}
}

// Context overflow after a side-effecting tool must not compact-and-replay.
func TestCompactionRecoverySkipsReplayAfterSideEffectingTool(t *testing.T) {
	overflow := &httpretry.APIError{
		StatusCode: 400,
		Message:    `{"error":{"message":"maximum context length is 200000 tokens","code":"context_length_exceeded"}}`,
	}
	if !isContextOverflow(overflow) {
		t.Fatal("precondition: context_length_exceeded must classify as overflow")
	}
	orig := &agent.AgentResult{StopReason: "end_turn", TotalToolCalls: 1, ToolCounts: map[string]int{"exec": 1}}
	tr := &fallbackTurn{
		logger:        discardLogger(),
		runErr:        overflow,
		agentResult:   orig,
		contextBudget: 12_000,
		messages:      []llm.Message{llm.NewTextMessage("user", "long turn")},
	}
	retry, stuck := tr.compactionRecovery(context.Background(), 0)
	if retry || stuck != nil {
		t.Fatalf("compactionRecovery = (retry=%v, stuck=%v), want (false, nil)", retry, stuck)
	}
}

// The notice must not claim tools finished when none ran — that would put a
// second false statement on top of the blank reply.
func TestFallbackForEmptyFinalReplyMatchesWhetherToolsRan(t *testing.T) {
	withTools := fallbackForEmptyFinalReply(true)
	if !strings.Contains(withTools, "도구 실행은 끝났는데") {
		t.Errorf("tool-round notice lost its wording: %q", withTools)
	}
	plain := fallbackForEmptyFinalReply(false)
	if strings.Contains(plain, "도구") {
		t.Errorf("single-shot notice claims tools ran: %q", plain)
	}
	if plain == "" || !strings.Contains(plain, "빈 응답") {
		t.Errorf("single-shot notice does not name the failure: %q", plain)
	}
}

func TestEmptyFinalResultRanToolsTracksRoundCount(t *testing.T) {
	if emptyFinalResultRanTools(&agent.AgentResult{Turns: 1}) {
		t.Error("a single round means no tool round ran")
	}
	if !emptyFinalResultRanTools(&agent.AgentResult{Turns: 2}) {
		t.Error("a tool round always leaves Turns >= 2")
	}
	if emptyFinalResultRanTools(nil) {
		t.Error("nil result must not claim tool activity")
	}
}
