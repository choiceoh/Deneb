package modelcaps

import "testing"

func TestIsOpenAIReasoningModel(t *testing.T) {
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
	for _, p := range []string{"kimi", "KIMI", " kimi ", "kimi-code", "kimi-coding", "Kimi-Subagent"} {
		if !RejectsCacheControl(p) {
			t.Errorf("RejectsCacheControl(%q) = false, want true", p)
		}
	}
	// "kimimaru" has no hyphen boundary; Anthropic-wire providers that accept
	// cache_control (mimo, zai) must not match.
	for _, p := range []string{"mimo", "mimo-plan", "zai", "anthropic", "openai", "kimimaru", ""} {
		if RejectsCacheControl(p) {
			t.Errorf("RejectsCacheControl(%q) = true, want false", p)
		}
	}
}

func TestBuiltin(t *testing.T) {
	c := Builtin("kimi", "kimi-for-coding")
	if !c.RejectsCacheControl || c.Reasoning {
		t.Errorf("kimi builtin = %+v, want RejectsCacheControl only", c)
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
