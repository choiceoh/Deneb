package server

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// TestDreamerLLMShape pins the request shaping that keeps the wiki dreamer
// alive across lightweight-model changes: the 2026-07-02/03 dream failures
// were dsv4 (dual-mode reasoning) spending the whole 4096-token synthesis
// budget on chain-of-thought because the dreamer's raw client calls carried
// no thinking-off kwarg.
func TestDreamerLLMShape(t *testing.T) {
	shape := func(lightweight string) (map[string]any, int) {
		t.Helper()
		reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
			MainModel:        "zai/main-model",
			LightweightModel: lightweight,
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

	t.Run("reasoning model with no off-switch gets budget headroom", func(t *testing.T) {
		extra, synthMax := shape("zai/deepseek-r1")
		if extra != nil {
			t.Errorf("extra = %v, want nil (no template toggle off vLLM)", extra)
		}
		if synthMax != 16384 {
			t.Errorf("synthMax = %d, want 16384", synthMax)
		}
	})

	t.Run("nil registry is a no-op", func(t *testing.T) {
		extra, synthMax := dreamerLLMShape(nil)
		if extra != nil || synthMax != 0 {
			t.Errorf("nil registry → (%v, %d), want (nil, 0)", extra, synthMax)
		}
	})
}
