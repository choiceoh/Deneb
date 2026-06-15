package modelrole

import (
	"log/slog"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func TestCapabilityForModel_Layering(t *testing.T) {
	srv := newDiscoverySrv(t, `{"data":[{"id":"gemma4","max_model_len":131072}]}`, 200)
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "vllm/gemma4",
		Providers: map[string]ProviderResolved{
			// vLLM carries a static contextWindow that now serves as a
			// FALLBACK — live /models discovery (max_model_len) wins over it.
			"vllm": {BaseURL: srv.URL + "/v1", ContextWindow: 262144},
			"kimi": {BaseURL: "https://api.kimi.example/coding", PromptCache: boolPtr(true)},
			"acme": {
				BaseURL:       "https://acme.example/v1",
				ContextWindow: 32768,
				Reasoning:     boolPtr(true),
				Vision:        boolPtr(false),
			},
		},
	})

	t.Run("vllm discovery wins over deneb.json contextWindow", func(t *testing.T) {
		// gemma4 is discovered at 131072; the vllm provider's static 262144 is
		// only a fallback, so discovery must win (the gateway tracks the server).
		caps := reg.CapabilityForModel("vllm", "gemma4")
		if caps.ContextWindow != 131072 {
			t.Errorf("ContextWindow = %d, want 131072 from discovery (over 262144 fallback)", caps.ContextWindow)
		}
		if caps.Reasoning || caps.NoVision || caps.RejectsCacheControl {
			t.Errorf("unexpected flags set: %+v", caps)
		}
	})

	t.Run("vllm model without discovery falls back to deneb.json window", func(t *testing.T) {
		// A vllm model the server never advertised (discovery empty) keeps the
		// static fallback so a startup-time probe failure degrades safely.
		caps := reg.CapabilityForModel("vllm", "not-served-model")
		if caps.ContextWindow != 262144 {
			t.Errorf("ContextWindow = %d, want 262144 fallback when discovery has nothing", caps.ContextWindow)
		}
	})

	t.Run("discovered window applies to vLLM-backed fronts, not cloud", func(t *testing.T) {
		// wormhole proxies the same vLLM serving, so a model fronted by it gets the
		// discovered window by served id (the regression fix); a cloud front of a
		// same-named model is a different serving and stays at 0.
		if caps := reg.CapabilityForModel("wormhole", "gemma4"); caps.ContextWindow != 131072 {
			t.Errorf("wormhole/gemma4 window = %d, want 131072 (vLLM-backed front)", caps.ContextWindow)
		}
		if caps := reg.CapabilityForModel("openrouter", "gemma4"); caps.ContextWindow != 0 {
			t.Errorf("openrouter/gemma4 window = %d, want 0 (cloud front, different serving)", caps.ContextWindow)
		}
	})

	t.Run("catalog overrides builtin defaults", func(t *testing.T) {
		// Kimi builtin says RejectsCacheControl; promptCache:true overrides it.
		if caps := reg.CapabilityForModel("kimi", "kimi-for-coding"); caps.RejectsCacheControl {
			t.Error("promptCache:true override should clear RejectsCacheControl")
		}
		caps := reg.CapabilityForModel("acme", "custom-model")
		if caps.ContextWindow != 32768 || !caps.Reasoning || !caps.NoVision {
			t.Errorf("acme caps = %+v, want window 32768, reasoning, no-vision", caps)
		}
	})

	t.Run("unknown provider resolves to builtin", func(t *testing.T) {
		caps := reg.CapabilityForModel("zai", "glm-5-turbo")
		if caps != reg.CapabilityForModel("zai", "glm-5-turbo") || caps.ContextWindow != 0 || caps.RejectsCacheControl {
			t.Errorf("zai caps = %+v, want permissive builtin", caps)
		}
	})
}

// TestCapabilityForModel_WormholeFrontedWindow covers the production regression:
// the main model is routed via the wormhole proxy (whose /v1/models omits
// max_model_len), yet the real window must still resolve — harvested from the
// still-configured direct vllm provider that no role uses, applied by served id.
func TestCapabilityForModel_WormholeFrontedWindow(t *testing.T) {
	srv := newDiscoverySrv(t, `{"data":[{"id":"deepseek-v4-flash","max_model_len":1000000}]}`, 200)
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		// Main is fronted by wormhole; no role uses the vllm provider directly.
		MainModel: "wormhole/deepseek-v4-flash",
		Providers: map[string]ProviderResolved{
			"wormhole": {BaseURL: "http://127.0.0.1:18800/v1"}, // proxy: no /models window
			"vllm":     {BaseURL: srv.URL + "/v1"},             // direct: reports the window
		},
	})

	if caps := reg.CapabilityForModel("wormhole", "deepseek-v4-flash"); caps.ContextWindow != 1000000 {
		t.Errorf("wormhole-fronted window = %d, want 1000000 harvested from the vllm provider", caps.ContextWindow)
	}
}

func TestProfileForModel_Layering(t *testing.T) {
	temp, topK := 0.3, 40
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "zai/glm-5-turbo",
		Providers: map[string]ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", Temperature: &temp, TopK: &topK},
		},
	})

	// Builtin profile passes through for providers without overrides.
	p := reg.ProfileForModel("vllm", "qwen3.6-35b")
	if p.Temperature == nil || *p.Temperature != 0.7 || !p.Reasoning {
		t.Errorf("qwen builtin profile = %+v, want temp 0.7 + reasoning", p)
	}

	// Provider overrides win over builtin; unset fields keep the lower layer.
	p = reg.ProfileForModel("acme", "qwen3.6-35b")
	if p.Temperature == nil || *p.Temperature != 0.3 {
		t.Errorf("temperature = %v, want 0.3 override", p.Temperature)
	}
	if p.TopK == nil || *p.TopK != 40 {
		t.Errorf("topK = %v, want 40 override", p.TopK)
	}
	if p.TopP == nil || *p.TopP != 0.8 {
		t.Errorf("topP = %v, want builtin 0.8 kept", p.TopP)
	}
	if !p.Reasoning {
		t.Error("reasoning-channel flag must stay builtin")
	}
}

func TestTunedMaxTokens(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{MainModel: "zai/glm-5-turbo"})
	if reg.TunedMaxTokens("m") != 0 {
		t.Fatal("unset model must report 0")
	}
	reg.SetTunedMaxTokens("m", 16384)
	if reg.TunedMaxTokens("m") != 16384 {
		t.Fatal("floor not stored")
	}
	reg.SetTunedMaxTokens("m", 0)
	if reg.TunedMaxTokens("m") != 0 {
		t.Fatal("zero must clear the floor")
	}
	reg.SetTunedMaxTokens("", 100) // must not panic or store
	if reg.TunedMaxTokens("") != 0 {
		t.Fatal("empty model must be ignored")
	}
}

func TestRefreshVllmRole(t *testing.T) {
	srv := newDiscoverySrv(t, `{"data":[{"id":"model-a","max_model_len":8192}]}`, 200)
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "vllm/model-a",
		Providers: map[string]ProviderResolved{"vllm": {BaseURL: srv.URL + "/v1"}},
	})

	t.Run("rate limit skips fresh probes", func(t *testing.T) {
		reg.mu.Lock()
		reg.vllmProbedAt[RoleMain] = time.Now()
		reg.mu.Unlock()
		if got := reg.RefreshVllmRole(RoleMain); got.Model != "model-a" {
			t.Errorf("model = %q, want unchanged under rate limit", got.Model)
		}
	})

	t.Run("stale probe picks up renamed served model", func(t *testing.T) {
		// Server now serves a different model; force the probe by aging the stamp.
		srv2 := newDiscoverySrv(t, `{"data":[{"id":"model-b","max_model_len":16384}]}`, 200)
		reg.mu.Lock()
		cfg := reg.models[RoleMain]
		cfg.BaseURL = srv2.URL + "/v1"
		reg.models[RoleMain] = cfg
		reg.vllmProbedAt[RoleMain] = time.Now().Add(-2 * vllmRefreshMinInterval)
		reg.mu.Unlock()

		got := reg.RefreshVllmRole(RoleMain)
		if got.Model != "model-b" {
			t.Fatalf("model = %q, want model-b after refresh", got.Model)
		}
		if caps := reg.CapabilityForModel("vllm", "model-b"); caps.ContextWindow != 16384 {
			t.Errorf("ContextWindow = %d, want 16384 harvested by refresh", caps.ContextWindow)
		}
	})

	t.Run("non-vllm role is a no-op", func(t *testing.T) {
		reg2 := NewRegistryWithOptions(slog.Default(), RegistryOptions{
			MainModel: "zai/glm-5-turbo",
		})
		if got := reg2.RefreshVllmRole(RoleMain); got.Model != "glm-5-turbo" {
			t.Errorf("model = %q, want unchanged", got.Model)
		}
	})
}
