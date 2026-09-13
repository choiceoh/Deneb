package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// A fallback chain must never ARRIVE at a pay-per-token model. wormhole enforces
// this on the routed path (cmd/wormhole/validate.go) after a dead local model
// billed 1,346 calls over twelve days in 2026-08. Pointing a role straight at
// the serving engine moves the failover decision into the gateway, where that
// rule had no counterpart — this is the counterpart.
func TestRunAgentWithFallback_SkipsMeteredCandidate(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		seen = append(seen, req.Model)
		mu.Unlock()

		if req.Model == "m-local" {
			w.WriteHeader(http.StatusInternalServerError) // the engine is down
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse("answered", "end_turn"))
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:        "test/m-local",
		CodingModel:      "test/m-metered", // would be next in main's chain
		LightweightModel: "test/m-twin",    // the unmetered cloud twin
		Providers: map[string]modelrole.ProviderResolved{
			"test": {BaseURL: server.URL, APIKey: "k"},
		},
		MeteredModels: map[string]bool{"m-metered": true},
	})

	cfg := agent.AgentConfig{Model: "m-local", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	_, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hello")},
		llm.NewClient(server.URL, "k"),
		runDeps{registry: reg, logger: discardLogger()},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, discardLogger(),
		agentlog.NewRunLogger(nil, "test-session", "test-run"),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !fellBack || actualModel != "m-twin" {
		t.Errorf("answered by %q (fellBack=%v), want m-twin", actualModel, fellBack)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, m := range seen {
		if m == "m-metered" {
			t.Fatalf("metered model was called: %v", seen)
		}
	}
	t.Logf("models attempted: %v", seen)
}

// Pointing a role AT a metered model is a deliberate act and still runs; only
// arriving at one by degradation is refused.
func TestRunAgentWithFallback_MeteredModelStillRunsWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse("paid answer", "end_turn"))
	}))
	defer server.Close()

	reg := modelrole.NewRegistryWithOptions(discardLogger(), modelrole.RegistryOptions{
		MainModel:     "test/m-metered",
		Providers:     map[string]modelrole.ProviderResolved{"test": {BaseURL: server.URL, APIKey: "k"}},
		MeteredModels: map[string]bool{"m-metered": true},
	})
	cfg := agent.AgentConfig{Model: "m-metered", MaxTurns: 2, Timeout: 5 * time.Second, MaxTokens: 128}
	_, actualModel, fellBack, err := runAgentWithFallback(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "hi")},
		llm.NewClient(server.URL, "k"),
		runDeps{registry: reg, logger: discardLogger()},
		"test", modelrole.RoleMain, nil, agent.StreamHooks{}, discardLogger(),
		agentlog.NewRunLogger(nil, "test-session", "test-run"),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fellBack || actualModel != "m-metered" {
		t.Errorf("actualModel=%q fellBack=%v, want the requested metered model to answer", actualModel, fellBack)
	}
}
