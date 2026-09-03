package modelcaps

import "testing"

func TestIsOpenAIReasoningModelAllowsKnownDeniesCompatible(t *testing.T) {
	for _, m := range []string{"o1", "o3-mini", "o4-mini-high", "gpt-5", "openai/o3-mini", "GPT-5-turbo", " o1-preview "} {
		if !IsOpenAIReasoningModel(m) {
			t.Errorf("IsOpenAIReasoningModel(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"gpt-4o", "gemma4", "glm-5-turbo", "step3p7", "kimi-for-coding", "olive-3b", ""} {
		if IsOpenAIReasoningModel(m) {
			t.Errorf("IsOpenAIReasoningModel(%q) = true, want false", m)
		}
	}
}

func TestRejectsCacheControl(t *testing.T) {
	// Kimi K2.7's coding endpoint now ACCEPTS cache_control (live-verified
	// 2026-07-17: repeat call returned cache_read_input_tokens with no 400),
	// so no provider strips markers anymore — the strip silently discarded
	// every prefix-cache hit on the subscription. The seam stays for the next
	// genuinely rejecting endpoint.
	for _, p := range []string{"kimi", "KIMI", "kimi-code", "kimi-subagent", "mimo", "mimo-plan", "zai", "anthropic", "openai", ""} {
		if RejectsCacheControl(p) {
			t.Errorf("RejectsCacheControl(%q) = true, want false", p)
		}
	}
}

func TestBuiltinMapsKnownProvidersReturnsZeroForUnknown(t *testing.T) {
	c := Builtin("kimi", "kimi-for-coding")
	if c.RejectsCacheControl || c.Reasoning {
		t.Errorf("kimi builtin = %+v, want fully permissive (markers accepted since K2.7)", c)
	}
	c = Builtin("openai", "o3-mini")
	if !c.Reasoning || c.RejectsCacheControl {
		t.Errorf("openai/o3-mini builtin = %+v, want Reasoning only", c)
	}
	// Unknown pair → fully permissive zero value (no clamping, no stripping).
	if c := Builtin("vllm", "gemma4"); c != (Capability{}) {
		t.Errorf("unknown builtin = %+v, want zero value", c)
	}
}

// TestThinkingToggleKwargAllowsVLLMDeniesOtherProviders verifies provider-aware gating: only vLLM-backed
// providers (a direct vllm provider OR the wormhole proxy) get the template
// toggle, and only for the DeepSeek V4 family.
func TestThinkingToggleKwargAllowsVLLMDeniesOtherProviders(t *testing.T) {
	for _, c := range []struct{ p, m string }{
		{"vllm", "deepseek-v4-flash"},
		{"vllm", "DeepSeek-V4-Flash"},     // case-insensitive
		{"wormhole", "deepseek-v4-flash"}, // main routed via wormhole/dsv4 must still route
		{"wormhole-vllm", "deepseek-v4"},  // wormhole alias prefix
	} {
		if got := ThinkingToggleKwarg(c.p, c.m); got != "thinking" {
			t.Errorf("ThinkingToggleKwarg(%q,%q) = %q, want \"thinking\"", c.p, c.m, got)
		}
	}
	for _, c := range []struct{ p, m string }{
		{"vllm", "step3p7"},                   // non-dual-mode model
		{"openrouter", "deepseek-v4"},         // non-vLLM provider must NOT get the kwarg
		{"deepseek", "deepseek-v4-flash"},     // official API is not vLLM serving
		{"", "deepseek-v4-flash"},             // no provider
		{"wormhole", "glm-5.2"},               // cloud model fronted by wormhole — model gate excludes
		{"wormhole", "mimo-v2.5-pro"},         // cloud model fronted by wormhole
		{"wormhole", "deepseek-v4-flash-api"}, // remote API alias, not the local vLLM model
	} {
		if got := ThinkingToggleKwarg(c.p, c.m); got != "" {
			t.Errorf("ThinkingToggleKwarg(%q,%q) = %q, want \"\"", c.p, c.m, got)
		}
	}
	if Builtin("wormhole", "deepseek-v4-flash").ThinkingToggleKwarg != "thinking" {
		t.Error("Builtin must surface the toggle kwarg for wormhole/dsv4")
	}
}

func TestIsRemoteAPIAlias(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash-api", "wormhole/deepseek-v4-pro-api", "deepseek-v4-flash-api-nothink"} {
		if !IsRemoteAPIAlias(model) {
			t.Errorf("IsRemoteAPIAlias(%q) = false, want true", model)
		}
	}
	for _, model := range []string{"deepseek-v4-flash", "api-compatible-model", ""} {
		if IsRemoteAPIAlias(model) {
			t.Errorf("IsRemoteAPIAlias(%q) = true, want false", model)
		}
	}
}
