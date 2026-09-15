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
	want := []Role{RoleTiny, RoleTinyFallback, RoleTinyFallbackPaid, RoleLightweight, RoleFallback}
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

func TestHelperFallbacksSkipBilledRungsButThePaidOne(t *testing.T) {
	providers := map[string]ProviderResolved{
		"openrouter": {BaseURL: "https://openrouter.example/api/v1", APIKey: "k"},
		"test":       {BaseURL: "http://127.0.0.1:1/v1", APIKey: "k"},
	}
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:             "test/tiny",
		TinyFallbackModel:     "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		TinyFallbackPaidModel: "openrouter/nvidia/nemotron-3-super-120b-a12b",
		LightweightModel:      "test/deepseek-v4-flash-api", // billed, nobody agreed
		FallbackModel:         "test/tiny",                  // same as tiny itself
		Providers:             providers,
		MeteredModels:         map[string]bool{"deepseek-v4-flash-api": true},
	})
	var models []string
	for _, fb := range reg.HelperFallbacks(RoleTiny) {
		if fb.Client == nil {
			t.Fatalf("rung %s has no client", fb.Role)
		}
		models = append(models, string(fb.Role)+"="+fb.Config.Model)
	}
	want := []string{
		"tinyfallback=nvidia/nemotron-3-super-120b-a12b:free",
		"tinyfallbackpaid=nvidia/nemotron-3-super-120b-a12b",
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("HelperFallbacks(tiny) = %v, want %v", models, want)
	}
	for role, skip := range map[Role]bool{RoleTinyFallback: false, RoleTinyFallbackPaid: false, RoleLightweight: true} {
		if got := reg.SkipBilledFallback(role); got != skip {
			t.Errorf("SkipBilledFallback(%s) = %v, want %v", role, got, skip)
		}
	}

	// A paid OpenRouter model in the free slot bills without anyone having
	// agreed to it there: it is skipped like any billed rung.
	regPaid := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b",
		LightweightModel:  "test/light",
		FallbackModel:     "test/tiny",
		Providers:         providers,
	})
	if got := regPaid.HelperFallbacks(RoleTiny); len(got) != 1 || got[0].Config.Model != "light" {
		t.Fatalf("HelperFallbacks with a paid model in the free slot = %+v, want only test/light", got)
	}

	// Unbilled rungs keep chain order.
	reg2 := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyModel:         "test/tiny",
		TinyFallbackModel: "openrouter/free-a:free",
		LightweightModel:  "test/light",
		FallbackModel:     "test/fb",
		Providers:         providers,
	})
	models = nil
	for _, fb := range reg2.HelperFallbacks(RoleTiny) {
		models = append(models, fb.Config.Model)
	}
	if !reflect.DeepEqual(models, []string{"free-a:free", "light", "fb"}) {
		t.Fatalf("HelperFallbacks order = %v", models)
	}

	var nilReg *Registry
	if nilReg.HelperFallbacks(RoleTiny) != nil || nilReg.SkipBilledFallback(RoleLightweight) {
		t.Fatal("nil registry returned fallbacks or skipped a rung")
	}
}

func TestTinyFallbackPaidRoleIsOptIn(t *testing.T) {
	providers := map[string]ProviderResolved{"openrouter": {BaseURL: "https://openrouter.example/api/v1", APIKey: "k"}}
	unset := NewRegistryWithOptions(slog.Default(), RegistryOptions{Providers: providers})
	if unset.Client(RoleTinyFallbackPaid) != nil {
		t.Fatal("tinyfallbackpaid has a client without agents.tinyFallbackPaidModel")
	}
	if _, _, ok := unset.ResolveModel("tinyfallbackpaid"); ok {
		t.Fatal("ResolveModel(tinyfallbackpaid) resolved while unconfigured")
	}
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		TinyFallbackPaidModel: "openrouter/nvidia/nemotron-3-super-120b-a12b",
		Providers:             providers,
	})
	if id, role, ok := reg.ResolveModel("tinyfallbackpaid"); !ok || role != RoleTinyFallbackPaid || id != "openrouter/nvidia/nemotron-3-super-120b-a12b" {
		t.Fatalf("ResolveModel(tinyfallbackpaid) = %q %q %v", id, role, ok)
	}
	if reg.Client(RoleTinyFallbackPaid) == nil {
		t.Fatal("configured tinyfallbackpaid has no client")
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
