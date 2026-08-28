package server

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// TestDreamerLLMShape pins the request shaping that keeps the wiki dreamer
// alive across tiny-model changes: the 2026-07-02/03 dream failures
// were dsv4 (dual-mode reasoning) spending the whole 4096-token synthesis
// budget on chain-of-thought because the dreamer's raw client calls carried
// no thinking-off kwarg.
func TestDreamerLLMShapeReturnsToggleAndBudgetByModel(t *testing.T) {
	if wikiDreamerModelRole != modelrole.RoleTiny {
		t.Fatalf("wiki dreamer role = %q, want tiny", wikiDreamerModelRole)
	}

	shape := func(tiny string) (map[string]any, int) {
		t.Helper()
		reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
			MainModel:        "zai/main-model",
			LightweightModel: "zai/unused-lightweight",
			TinyModel:        tiny,
			// Hermetic endpoints: registry construction probes vLLM-backed
			// providers for /v1/models discovery — an unroutable loopback
			// port fails instantly on any host, so the test neither dials a
			// live dev vLLM nor waits on a discovery timeout.
			Providers: map[string]modelrole.ProviderResolved{
				"vllm":     {BaseURL: "http://127.0.0.1:1/v1"},
				"wormhole": {BaseURL: "http://127.0.0.1:1/v1"},
			},
		})
		return dreamerLLMShape(reg)
	}

	assertToggle := func(t *testing.T, extra map[string]any, kwarg string) {
		t.Helper()
		kwargs, ok := extra["chat_template_kwargs"].(map[string]any)
		if !ok {
			t.Fatalf("extra = %v, want chat_template_kwargs map", extra)
		}
		if v, ok := kwargs[kwarg].(bool); !ok || v {
			t.Fatalf("chat_template_kwargs[%s] = %v, want false", kwarg, kwargs[kwarg])
		}
	}

	t.Run("dsv4 on vllm gets the thinking toggle", func(t *testing.T) {
		extra, synthMax := shape("vllm/deepseek-v4-flash")
		assertToggle(t, extra, "thinking")
		if synthMax != 0 {
			t.Errorf("synthMax = %d, want 0 (default budget once thinking is off)", synthMax)
		}
	})

	t.Run("dsv4 fronted by wormhole gets the same toggle", func(t *testing.T) {
		extra, _ := shape("wormhole/deepseek-v4-flash")
		assertToggle(t, extra, "thinking")
	})

	t.Run("srv4 tiny alias stays thinking off", func(t *testing.T) {
		extra, synthMax := shape("wormhole/dsv4-nothink")
		assertToggle(t, extra, "enable_thinking")
		if synthMax != 0 {
			t.Errorf("synthMax = %d, want 0", synthMax)
		}
	})

	t.Run("non-reasoning model mirrors the hub NoThinking kwargs", func(t *testing.T) {
		// qwen3 *-instruct variants ship with thinking disabled (Profile
		// Reasoning=false) and no template toggle → hub-style NoThinking.
		extra, synthMax := shape("vllm/qwen3.6-35b-instruct")
		assertToggle(t, extra, "enable_thinking")
		if synthMax != 0 {
			t.Errorf("synthMax = %d, want 0", synthMax)
		}
	})

	t.Run("reasoning qwen3 without a toggle gets budget headroom", func(t *testing.T) {
		extra, synthMax := shape("vllm/qwen3.6-35b")
		if extra != nil {
			t.Errorf("extra = %v, want nil (no template toggle for qwen3 in modelcaps)", extra)
		}
		if synthMax != 16384 {
			t.Errorf("synthMax = %d, want 16384", synthMax)
		}
	})

	t.Run("glm behind wormhole gets native reasoning headroom", func(t *testing.T) {
		extra, synthMax := shape("wormhole/glm-5.3")
		if extra != nil {
			t.Errorf("extra = %v, want nil so the typed thinking-off signal reaches wormhole", extra)
		}
		if synthMax != 16384 {
			t.Errorf("synthMax = %d, want 16384", synthMax)
		}
	})

	t.Run("reasoning model with no off-switch gets budget headroom", func(t *testing.T) {
		extra, synthMax := shape("zai/deepseek-r1")
		if extra != nil {
			t.Errorf("extra = %v, want nil (no template toggle off vLLM)", extra)
		}
		if synthMax != 16384 {
			t.Errorf("synthMax = %d, want 16384", synthMax)
		}
	})

	t.Run("non-reasoning cloud model is left unshaped, no headroom", func(t *testing.T) {
		// chat_template_kwargs is a vLLM serving feature; a direct cloud
		// provider must get neither NoThinking kwargs (unknown-field 400
		// risk) nor the reasoning headroom (nothing to budget for).
		extra, synthMax := shape("zai/foo-chat")
		if extra != nil {
			t.Errorf("extra = %v, want nil off vLLM-backed providers", extra)
		}
		if synthMax != 0 {
			t.Errorf("synthMax = %d, want 0", synthMax)
		}
	})

	t.Run("deneb.json reasoning:true override gets budget headroom", func(t *testing.T) {
		// A model the builtin prefix table doesn't know, declared a reasoning
		// endpoint by its provider's deneb.json entry (CapabilityForModel
		// layering) — must budget like a builtin reasoning model.
		yes := true
		reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
			MainModel:        "zai/main-model",
			LightweightModel: "zai/unused-lightweight",
			TinyModel:        "mycloud/mystery-reasoner",
			Providers: map[string]modelrole.ProviderResolved{
				"mycloud": {BaseURL: "http://127.0.0.1:1/v1", Reasoning: &yes},
			},
		})
		extra, synthMax := dreamerLLMShape(reg)
		if extra != nil {
			t.Errorf("extra = %v, want nil (no template toggle off vLLM)", extra)
		}
		if synthMax != 16384 {
			t.Errorf("synthMax = %d, want 16384 (provider-declared reasoning)", synthMax)
		}
	})

	t.Run("routing.toggleKwarg override shapes a config-declared dual-mode model", func(t *testing.T) {
		kw := "custom_thinking"
		reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
			MainModel:        "zai/main-model",
			LightweightModel: "zai/unused-lightweight",
			TinyModel:        "myvllm/custom-dual-mode",
			Providers: map[string]modelrole.ProviderResolved{
				"myvllm": {BaseURL: "http://127.0.0.1:1/v1", Routing: &modelrole.RoutingOverride{ToggleKwarg: &kw}},
			},
		})
		extra, synthMax := dreamerLLMShape(reg)
		assertToggle(t, extra, kw)
		if synthMax != 0 {
			t.Errorf("synthMax = %d, want 0 (toggle available)", synthMax)
		}
	})

	t.Run("nil registry is a no-op", func(t *testing.T) {
		extra, synthMax := dreamerLLMShape(nil)
		if extra != nil || synthMax != 0 {
			t.Errorf("nil registry → (%v, %d), want (nil, 0)", extra, synthMax)
		}
	})
}

func TestDreamerSynthesisFallbackTargetsFollowTinyChain(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:        "zai/main-model",
		LightweightModel: "wormhole/deepseek-v4-flash",
		TinyModel:        "wormhole/dsv4-nothink",
		FallbackModel:    "wormhole/glm-5.3",
		Providers: map[string]modelrole.ProviderResolved{
			"wormhole": {BaseURL: "http://127.0.0.1:1/v1"},
		},
	})

	targets := dreamerSynthesisFallbackTargets(reg)
	if len(targets) != 2 {
		t.Fatalf("fallback targets = %+v, want lightweight and fallback", targets)
	}
	if targets[0].Label != "lightweight" || targets[0].Model != "deepseek-v4-flash" {
		t.Fatalf("first fallback = label=%q model=%q, want lightweight/deepseek-v4-flash",
			targets[0].Label, targets[0].Model)
	}
	if targets[1].Label != "fallback" || targets[1].Model != "glm-5.3" {
		t.Fatalf("second fallback = label=%q model=%q, want fallback/glm-5.3",
			targets[1].Label, targets[1].Model)
	}
	if targets[1].SynthesisMaxTokens != 16384 {
		t.Fatalf("glm fallback synthesis max = %d, want reasoning headroom", targets[1].SynthesisMaxTokens)
	}
}
