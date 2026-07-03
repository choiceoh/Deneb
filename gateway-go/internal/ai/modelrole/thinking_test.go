package modelrole

import "testing"

// TestThinkingOffExtraBody pins the shared raw-call thinking-off contract
// consumed by the localai hub, the pilot direct path, and the wiki dreamer
// wiring. The dual-mode case is the load-bearing one: deepseek-v4 keeps
// Profile.Reasoning=false by design, so a reasoning-first check would send it
// the Qwen enable_thinking spelling its template ignores.
func TestThinkingOffExtraBody(t *testing.T) {
	toggleOf := func(extra map[string]any) map[string]any {
		t.Helper()
		if extra == nil {
			t.Fatal("extra = nil, want kwargs")
		}
		ctk, ok := extra["chat_template_kwargs"].(map[string]any)
		if !ok {
			t.Fatalf("chat_template_kwargs missing: %v", extra)
		}
		return ctk
	}

	t.Run("dsv4 on vLLM-backed providers gets its template toggle", func(t *testing.T) {
		for _, provider := range []string{"vllm", "wormhole"} {
			ctk := toggleOf(ThinkingOffExtraBody(provider, "deepseek-v4-flash"))
			if ctk["thinking"] != false {
				t.Errorf("%s: thinking = %v, want false", provider, ctk["thinking"])
			}
			if _, has := ctk["enable_thinking"]; has {
				t.Errorf("%s: enable_thinking must not be sent to dsv4", provider)
			}
		}
	})

	t.Run("untoggleable reasoning models get nil (budget for thinking)", func(t *testing.T) {
		for _, model := range []string{"qwen3.6-35b-a3b", "deepseek-r1", "step3p7"} {
			if got := ThinkingOffExtraBody("vllm", model); got != nil {
				t.Errorf("%s: extra = %v, want nil", model, got)
			}
		}
	})

	t.Run("non-reasoning models keep the NoThinkingBody kwargs", func(t *testing.T) {
		for _, model := range []string{"gemma4", "qwen3.6-35b-instruct"} {
			ctk := toggleOf(ThinkingOffExtraBody("vllm", model))
			if ctk["enable_thinking"] != false {
				t.Errorf("%s: enable_thinking = %v, want false", model, ctk["enable_thinking"])
			}
		}
	})

	t.Run("direct cloud providers are left unshaped", func(t *testing.T) {
		// chat_template_kwargs is a vLLM serving feature: a strict OpenAI-compat
		// API can 400 on the unknown field, so off vLLM-backed providers the
		// three-way now returns nil for non-reasoning models too (the hub's
		// historical NoThinkingBody used to leak to cloud lightweight configs).
		for _, model := range []string{"deepseek-v4-flash", "gemma4"} {
			if got := ThinkingOffExtraBody("zai", model); got != nil {
				t.Errorf("zai/%s: extra = %v, want nil off vLLM-backed providers", model, got)
			}
		}
	})
}
