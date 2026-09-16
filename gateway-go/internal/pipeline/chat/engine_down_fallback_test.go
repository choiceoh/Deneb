package chat

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

// "엔진이 죽어있으면 재시도 하지말고 바로 폴백으로" (operator, 2026-09-15).
// On 2026-09-14 a turn that reached the dead local engine waited out a
// six-attempt retry ladder (~70s), then a run-level replay of the same ladder,
// before the fallback chain started. These tests pin the path once a readiness
// probe has reported the engine down: no attempt on it, no replay, no hop onto
// another model it serves, and no streak blamed on it.

const downEngine = "http://10.0.0.5:8000/metrics"

// modelRecorder is a test server answering every model with a reply, recording
// which models were asked, and failing the test if a forbidden one is.
func modelRecorder(t *testing.T, forbidden ...string) (*httptest.Server, func() []string) {
	t.Helper()
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
		for _, f := range forbidden {
			if req.Model == f {
				t.Errorf("model %q was requested although its engine is reported down", f)
				w.WriteHeader(http.StatusBadGateway)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse("fallback reply", "end_turn"))
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), models...)
	}
}

func TestRunAgentWithFallback_EngineDownGoesStraightToFallback(t *testing.T) {
	server, asked := modelRecorder(t, "m-local")
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		LightweightModel: "test/m-local",
		FallbackModel:    "test/m-cloud",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	reg.SetEngineDown(downEngine, []string{"m-local"})

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := agent.AgentConfig{Model: "m-local", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	start := time.Now()
	result, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
		llm.NewClient(server.URL, "test-key"),
		runDeps{registry: reg, logger: logger},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, agentlog.NewRunLogger(nil, "s", "r"),
	)
	if err != nil {
		t.Fatalf("err = %v, want the fallback's answer", err)
	}
	if result == nil || result.Text != "fallback reply" || actualModel != "m-cloud" || !fellBack {
		t.Fatalf("result=%+v actualModel=%q fellBack=%v, want the fallback's reply from m-cloud", result, actualModel, fellBack)
	}
	if got := strings.Join(asked(), ","); got != "m-cloud" {
		t.Fatalf("models asked = %q, want only the fallback", got)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("turn took %v; nothing should wait on a refusing engine", elapsed)
	}
	for _, noisy := range []string{"retrying LLM request", "transient HTTP error, retrying once", `level=WARN msg="model failed, trying fallback"`} {
		if strings.Contains(logs.String(), noisy) {
			t.Errorf("log contains %q — the engine's outage was already announced by the watcher:\n%s", noisy, logs.String())
		}
	}
	// The skip must say why. "circuit open: skipped after repeated recent
	// failures" would be false — nothing failed; the engine was reported down.
	if !strings.Contains(logs.String(), "serving engine down; skipping straight to fallback chain") ||
		!strings.Contains(logs.String(), "serving backend is refusing requests: m-local") {
		t.Errorf("skip was not attributed to the engine being down:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "repeated recent failures") {
		t.Errorf("skip was attributed to a failure streak that never happened:\n%s", logs.String())
	}
}

// TestRunAgentWithFallback_EngineDownWithOnlyMeteredFallbackFailsFast is the
// production shape of 2026-09-15 08:21: a role on the down engine whose only
// remaining candidates are metered. The chain will not arrive at those (dogma
// #7), so the turn cannot be answered — it must say so at once, and say why,
// rather than spend a retry ladder first or blame a failure streak.
func TestRunAgentWithFallback_EngineDownWithOnlyMeteredFallbackFailsFast(t *testing.T) {
	server, asked := modelRecorder(t, "m-local", "m-metered")
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		LightweightModel: "test/m-metered",
		FallbackModel:    "test/m-metered",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
		MeteredModels: map[string]bool{"m-metered": true},
	})
	reg.SetEngineDown(downEngine, []string{"m-local"})
	// Production hands the chat pipeline the registry's own client, which
	// carries the gate.
	gated := llm.NewClient(server.URL, "test-key", llm.WithBackendDownCheck(reg.EngineDown))

	cfg := agent.AgentConfig{Model: "m-local", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	start := time.Now()
	_, _, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
		gated,
		runDeps{registry: reg, logger: discardLogger()},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, discardLogger(), agentlog.NewRunLogger(nil, "s", "r"),
	)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("turn took %v; with nothing reachable it must fail at once", elapsed)
	}
	if fellBack {
		t.Fatal("fellBack = true, but the only candidates are metered")
	}
	if !errors.Is(err, llm.ErrBackendDown) {
		t.Fatalf("err = %v, want ErrBackendDown — the failure must name the engine, not a circuit streak", err)
	}
	if got := asked(); len(got) != 0 {
		t.Fatalf("models asked = %v, want none", got)
	}
}

func TestRunAgentWithFallback_EngineDownSkipsSameEngineRungInChain(t *testing.T) {
	// tiny → lightweight → fallback: tiny and lightweight live on the same local
	// engine, so a dead engine used to be reached twice in one turn.
	server, asked := modelRecorder(t, "m-local-low", "m-local")
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		TinyModel:        "test/m-local-low",
		LightweightModel: "test/m-local",
		FallbackModel:    "test/m-cloud",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	reg.SetEngineDown(downEngine, []string{"m-local", "m-local-low"})

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := agent.AgentConfig{Model: "m-local-low", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	_, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
		llm.NewClient(server.URL, "test-key"),
		runDeps{registry: reg, logger: logger},
		"test", modelrole.RoleTiny, nil, agent.StreamHooks{}, logger, agentlog.NewRunLogger(nil, "s", "r"),
	)
	if err != nil || actualModel != "m-cloud" || !fellBack {
		t.Fatalf("err=%v actualModel=%q fellBack=%v, want the fallback", err, actualModel, fellBack)
	}
	if got := strings.Join(asked(), ","); got != "m-cloud" {
		t.Fatalf("models asked = %q, want only the fallback", got)
	}
	out := logs.String()
	if !strings.Contains(out, "skipping fallback candidate: serving engine down") || !strings.Contains(out, "model=m-local") {
		t.Errorf("the lightweight rung on the dead engine must be skipped, not attempted:\n%s", out)
	}
	if strings.Contains(out, "nextModel=m-local ") || strings.Contains(out, "nextModel=m-local\n") {
		t.Errorf("the chain tried the lightweight rung on the dead engine:\n%s", out)
	}
}

func TestRunAgentWithFallback_GateRefusalDoesNotFeedTheBreaker(t *testing.T) {
	// The primary's client is gated but the registry was not told at turn start
	// (the report landed mid-turn): the initial attempt is refused by the
	// client. That refusal is not a model fault, and three of them must not open
	// a breaker that would outlive the engine's recovery.
	server, asked := modelRecorder(t, "m-local")
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		LightweightModel: "test/m-local",
		FallbackModel:    "test/m-cloud",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	gated := llm.NewClient(server.URL, "test-key",
		llm.WithBackendDownCheck(func(model string) bool { return model == "m-local" }))

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	for turn := range unhealthyTurns() {
		cfg := agent.AgentConfig{Model: "m-local", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
		_, actualModel, fellBack, err := runAgentWithFallback(
			context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
			gated,
			runDeps{registry: reg, logger: logger},
			"test", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, agentlog.NewRunLogger(nil, "s", "r"),
		)
		if err != nil || actualModel != "m-cloud" || !fellBack {
			t.Fatalf("turn %d: err=%v actualModel=%q fellBack=%v, want the fallback", turn, err, actualModel, fellBack)
		}
	}
	if reg.ModelUnhealthy("m-local") {
		t.Error("gate refusals opened m-local's breaker; it would stay on fallback for the whole cooldown after the engine returned")
	}
	for _, m := range asked() {
		if m != "m-cloud" {
			t.Fatalf("models asked = %v, want only the fallback", asked())
		}
	}
	if strings.Contains(logs.String(), "transient HTTP error, retrying once") {
		t.Errorf("a gate refusal was replayed as a transient error:\n%s", logs.String())
	}
}

// unhealthyTurns is enough turns to open a breaker if each fed it.
func unhealthyTurns() int { return 4 }

func TestRetryTransientSkipsReplayWhenEngineDown(t *testing.T) {
	transient := &httpretry.APIError{StatusCode: http.StatusBadGateway, Message: "upstream unreachable: m-local"}
	if !isTransientLLMError(transient) {
		t.Fatal("precondition: a 502 must classify as transient, else the guard under test is never reached")
	}
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:     "zai/m-local",
		FallbackModel: "zai/m-cloud",
	})
	reg.SetEngineDown(downEngine, []string{"m-local"})
	// client left nil on purpose: a replay would call agent.RunAgent with it and
	// panic, so a clean return proves the skip.
	tr := &fallbackTurn{
		logger: discardLogger(),
		deps:   runDeps{registry: reg},
		cfg:    agent.AgentConfig{Model: "m-local"},
		runErr: transient,
	}

	aborted, err := tr.retryTransient(context.Background())
	if aborted || err != nil {
		t.Fatalf("retryTransient = (%v, %v), want (false, nil)", aborted, err)
	}
	if !errors.Is(tr.runErr, transient) {
		t.Error("runErr changed — the turn was replayed against an engine reported down")
	}
}

func TestIsTransientLLMErrorRejectsBackendDown(t *testing.T) {
	refusal := fmt.Errorf("stream chat (turn 0): %w", fmt.Errorf("%w: m-local", llm.ErrBackendDown))
	if isTransientLLMError(refusal) {
		t.Fatal("a liveness-gate refusal classified as transient: the run would be replayed into the same refusal")
	}

	// The decision is by identity, not by what else the error carries. A refusal
	// that also wraps the last upstream 502 — the natural thing for a later
	// change to add for diagnostics — must still not be replayed; the classifier
	// alone would find the 502 and call it transient.
	last := &httpretry.APIError{StatusCode: http.StatusBadGateway, Message: "upstream unreachable: m-local"}
	if !isTransientLLMError(fmt.Errorf("stream chat (turn 0): %w", last)) {
		t.Fatal("precondition: the wrapped 502 must classify as transient on its own, or the identity check is not being tested")
	}
	annotated := fmt.Errorf("stream chat (turn 0): %w", fmt.Errorf("%w: m-local (last attempt: %w)", llm.ErrBackendDown, last))
	if isTransientLLMError(annotated) {
		t.Fatal("a refusal carrying the last 502 classified as transient: the identity check is missing")
	}
}

// TestResolveClient_ConfiguredProviderCarriesEngineDownGate pins the path the
// first live check caught: production turns resolve their client from the
// deneb.json provider config ("using provider from config"), not from the
// registry, so a gate installed only on registry clients never fired — a turn
// on a model already reported down ran the whole retry ladder (79.6s).
func TestResolveClient_ConfiguredProviderCarriesEngineDownGate(t *testing.T) {
	var calls sync.Mutex
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Lock()
		hits++
		calls.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel: "zai/m-cloud",
	})
	reg.SetEngineDown(downEngine, []string{"m-local"})
	deps := runDeps{
		registry:        reg,
		providerConfigs: map[string]ProviderConfig{"wormhole": {BaseURL: server.URL}},
	}

	client := resolveClient(context.Background(), deps, "wormhole", discardLogger())
	if client == nil || client.BaseURL() != server.URL {
		t.Fatalf("precondition: the client must come from the provider config (got %v)", client)
	}
	start := time.Now()
	_, err := client.StreamChat(context.Background(), llm.ChatRequest{
		Model:    "m-local",
		Messages: []llm.Message{llm.NewTextMessage("user", "hello")},
	})
	if !errors.Is(err, llm.ErrBackendDown) {
		t.Fatalf("err = %v, want ErrBackendDown from the config-resolved client", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("refusal took %v, want immediate", elapsed)
	}
	calls.Lock()
	defer calls.Unlock()
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0", hits)
	}
}

// A provider configured as openrouter in deneb.json resolves to a client built
// here, not by the registry — it needs the reasoning field as much as the
// registry's clients do.
func TestResolveClient_OpenRouterConfigClientSendsReasoningField(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := resolveClient(context.Background(), runDeps{
		providerConfigs: map[string]ProviderConfig{"openrouter": {BaseURL: server.URL}},
	}, "openrouter", discardLogger())
	if _, err := client.Complete(context.Background(), llm.ChatRequest{
		Model:    "nvidia/nemotron-3-super-120b-a12b:free",
		Messages: []llm.Message{llm.NewTextMessage("user", "hi")},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}); err != nil {
		t.Fatal(err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["enabled"] != false || body["reasoning_effort"] != nil {
		t.Fatalf("request body = %v, want reasoning.enabled=false and no reasoning_effort", body)
	}
}

// The done frame says WHY the fallback fired, so the answer can read "engine
// down" rather than only naming the substitute. An engine reported down is
// "engine_down" — never the circuit-open story, which would blame a failure
// streak that never happened.
func TestRunAgentWithFallbackDetailed_EngineDownReportsItsReason(t *testing.T) {
	server, _ := modelRecorder(t, "m-local")
	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		LightweightModel: "test/m-local",
		FallbackModel:    "test/m-cloud",
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
	})
	reg.SetEngineDown(downEngine, []string{"m-local"})
	logger := discardLogger()
	cfg := agent.AgentConfig{Model: "m-local", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	result, actualModel, fellBack, reason, err := runAgentWithFallbackDetailed(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
		llm.NewClient(server.URL, "test-key"),
		runDeps{registry: reg, logger: logger},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, logger, agentlog.NewRunLogger(nil, "s", "r"),
	)
	if err != nil || result == nil || actualModel != "m-cloud" || !fellBack {
		t.Fatalf("result=%+v actualModel=%q fellBack=%v err=%v", result, actualModel, fellBack, err)
	}
	if reason != FallbackReasonEngineDown {
		t.Fatalf("reason = %q, want %q", reason, FallbackReasonEngineDown)
	}
}
