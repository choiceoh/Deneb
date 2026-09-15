package modelrole

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestTinyFallbackRoleIsOptInAndSitsFirstInTinysChain(t *testing.T) {
	providers := map[string]ProviderResolved{
		"openrouter": {BaseURL: "https://openrouter.example/api/v1", APIKey: "k"},
		"test":       {BaseURL: "http://127.0.0.1:1/v1", APIKey: "k"},
	}

	unset := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel: "test/tiny", LightweightModel: "test/light", FallbackModel: "test/fb", Providers: providers,
	})
	if unset.Client(RoleTinyFallback) != nil {
		t.Fatal("tinyfallback has a client without agents.tinyFallbackModel — an absent role must stay absent")
	}
	if _, role, ok := unset.ResolveModel("tinyfallback"); ok {
		t.Fatalf("ResolveModel(tinyfallback) resolved to %q while unconfigured", role)
	}

	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		LightweightModel:  "test/light",
		FallbackModel:     "test/fb",
		Providers:         providers,
	})
	want := []Role{RoleTiny, RoleTinyFallback, RoleLightweight, RoleFallback}
	if got := reg.FallbackChain(RoleTiny); !reflect.DeepEqual(got, want) {
		t.Fatalf("FallbackChain(tiny) = %v, want %v", got, want)
	}
	cfg := reg.Config(RoleTinyFallback)
	// Everything after the first "/" is the model: OpenRouter ids carry their own.
	if cfg.ProviderID != "openrouter" || cfg.Model != "nvidia/nemotron-3-super-120b-a12b:free" || cfg.BaseURL != "https://openrouter.example/api/v1" {
		t.Fatalf("tinyfallback config = %+v", cfg)
	}
	if reg.Client(RoleTinyFallback) == nil {
		t.Fatal("configured tinyfallback has no client")
	}
	if id, role, ok := reg.ResolveModel("tinyfallback"); !ok || role != RoleTinyFallback || id != "openrouter/nvidia/nemotron-3-super-120b-a12b:free" {
		t.Fatalf("ResolveModel(tinyfallback) = %q %q %v", id, role, ok)
	}
	// Only tiny's chain gains the rung.
	if got := reg.FallbackChain(RoleLightweight); !reflect.DeepEqual(got, []Role{RoleLightweight, RoleFallback}) {
		t.Fatalf("FallbackChain(lightweight) = %v, want it unchanged", got)
	}
}

func TestUnmeteredFallbacksLeavesOutBilledAndDuplicateRungs(t *testing.T) {
	providers := map[string]ProviderResolved{
		"openrouter": {BaseURL: "https://openrouter.example/api/v1", APIKey: "k"},
		"test":       {BaseURL: "http://127.0.0.1:1/v1", APIKey: "k"},
	}
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		LightweightModel:  "test/deepseek-v4-flash-api", // billed
		FallbackModel:     "test/tiny",                  // same as tiny itself
		Providers:         providers,
		MeteredModels:     map[string]bool{"deepseek-v4-flash-api": true},
	})
	got := reg.UnmeteredFallbacks(RoleTiny)
	if len(got) != 1 || got[0].Role != RoleTinyFallback || got[0].Config.Model != "nvidia/nemotron-3-super-120b-a12b:free" || got[0].Client == nil {
		t.Fatalf("UnmeteredFallbacks(tiny) = %+v, want only the free tiny fallback", got)
	}

	// A paid OpenRouter model bills from the account's credits without ever
	// passing the router, so the wormhole metered set cannot name it.
	regPaid := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b",
		LightweightModel:  "test/light",
		FallbackModel:     "test/tiny",
		Providers:         providers,
	})
	if got := regPaid.UnmeteredFallbacks(RoleTiny); len(got) != 1 || got[0].Config.Model != "light" {
		t.Fatalf("UnmeteredFallbacks with a paid OpenRouter rung = %+v, want only test/light", got)
	}

	// Unmetered rungs keep chain order.
	reg2 := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/free-a:free",
		LightweightModel:  "test/light",
		FallbackModel:     "test/fb",
		Providers:         providers,
	})
	var models []string
	for _, fb := range reg2.UnmeteredFallbacks(RoleTiny) {
		models = append(models, fb.Config.Model)
	}
	if !reflect.DeepEqual(models, []string{"free-a:free", "light", "fb"}) {
		t.Fatalf("UnmeteredFallbacks order = %v", models)
	}

	var nilReg *Registry
	if nilReg.UnmeteredFallbacks(RoleTiny) != nil {
		t.Fatal("nil registry returned fallbacks")
	}
}

// The registry's OpenRouter clients must carry the reasoning field for requests
// that disable thinking — stage-1 extraction sends Thinking{disabled}.
func TestRegistryOpenRouterClientSendsReasoningField(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "zai/tiny",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		Providers:         map[string]ProviderResolved{"openrouter": {BaseURL: server.URL, APIKey: "k"}},
	})
	client := reg.Client(RoleTinyFallback)
	if client == nil {
		t.Fatal("no client")
	}
	if _, err := client.Complete(context.Background(), llm.ChatRequest{
		Model:    "nvidia/nemotron-3-super-120b-a12b:free",
		Messages: []llm.Message{llm.NewTextMessage("user", "hi")},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}); err != nil {
		t.Fatal(err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["enabled"] != false || body["reasoning_effort"] != nil {
		t.Fatalf("openrouter request body = %v, want reasoning.enabled=false and no reasoning_effort", body)
	}
}
