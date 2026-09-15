package modelrole

import (
	"log/slog"
	"reflect"
	"testing"
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
