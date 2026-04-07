package thinking

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

// mockResolver implements ProviderThinkingResolver for testing.
type mockResolver struct {
	binaryResult  *bool
	xhighResult   *bool
	defaultResult *types.ThinkLevel
}

func (m *mockResolver) ResolveBinaryThinking(provider string, ctx ProviderThinkingContext) (bool, bool) {
	if m.binaryResult != nil {
		return *m.binaryResult, true
	}
	return false, false
}

func (m *mockResolver) ResolveXHighThinking(provider string, ctx ProviderThinkingContext) (bool, bool) {
	if m.xhighResult != nil {
		return *m.xhighResult, true
	}
	return false, false
}

func (m *mockResolver) ResolveDefaultThinkingLevel(provider string, ctx ProviderThinkingContext) (types.ThinkLevel, bool) {
	if m.defaultResult != nil {
		return *m.defaultResult, true
	}
	return "", false
}

func thinkingBoolPtr(v bool) *bool                  { return &v }
func thinkPtr(v types.ThinkLevel) *types.ThinkLevel { return &v }

func TestThinkingRuntime_IsBinaryThinkingProvider_Builtin(t *testing.T) {
	rt := NewThinkingRuntime()
	// zai is binary via built-in.
	if !rt.IsBinaryThinkingProvider("zai", "") {
		t.Error("expected zai to be binary")
	}
	if rt.IsBinaryThinkingProvider("openai", "") {
		t.Error("expected openai to not be binary")
	}
}

func TestThinkingRuntime_IsBinaryThinkingProvider_Plugin(t *testing.T) {
	rt := NewThinkingRuntime()
	rt.SetResolver(&mockResolver{binaryResult: thinkingBoolPtr(true)})

	// Plugin says "custom-provider" is binary.
	if !rt.IsBinaryThinkingProvider("custom-provider", "model-1") {
		t.Error("expected plugin to override: binary=true")
	}
}

func TestThinkingRuntime_SupportsXHighThinking_Builtin(t *testing.T) {
	rt := NewThinkingRuntime()
	if !rt.SupportsXHighThinking("openai", "gpt-5.4") {
		t.Error("expected xhigh support for openai/gpt-5.4")
	}
	if rt.SupportsXHighThinking("anthropic", "claude-opus-4.6") {
		t.Error("expected no xhigh for anthropic")
	}
}

func TestThinkingRuntime_SupportsXHighThinking_Plugin(t *testing.T) {
	rt := NewThinkingRuntime()
	rt.SetResolver(&mockResolver{xhighResult: thinkingBoolPtr(true)})

	// Plugin says custom model supports xhigh.
	if !rt.SupportsXHighThinking("custom", "model-x") {
		t.Error("expected plugin to override: xhigh=true")
	}
}

func TestThinkingRuntime_ResolveDefault_Builtin(t *testing.T) {
	rt := NewThinkingRuntime()
	level := rt.ResolveThinkingDefaultForModel("anthropic", "claude-opus-4.6", nil)
	if level != types.ThinkAdaptive {
		t.Errorf("expected adaptive for claude 4.6, got %q", level)
	}
}

func TestThinkingRuntime_ResolveDefault_Plugin(t *testing.T) {
	rt := NewThinkingRuntime()
	rt.SetResolver(&mockResolver{defaultResult: thinkPtr(types.ThinkHigh)})

	level := rt.ResolveThinkingDefaultForModel("custom", "model-x", nil)
	if level != types.ThinkHigh {
		t.Errorf("expected plugin to override: high, got %q", level)
	}
}

func TestThinkingRuntime_ListLevels_WithXHigh(t *testing.T) {
	rt := NewThinkingRuntime()
	levels := rt.ListThinkingLevels("openai", "gpt-5.4")
	found := false
	for _, l := range levels {
		if l == types.ThinkXHigh {
			found = true
		}
	}
	if !found {
		t.Error("expected xhigh in levels for openai/gpt-5.4")
	}
}

func TestThinkingRuntime_ListLabels_Binary(t *testing.T) {
	rt := NewThinkingRuntime()
	labels := rt.ListThinkingLevelLabels("zai", "")
	if len(labels) != 2 || labels[0] != "off" || labels[1] != "on" {
		t.Errorf("expected [off, on] for binary provider, got %v", labels)
	}
}

func TestThinkingRuntime_FormatLevels(t *testing.T) {
	rt := NewThinkingRuntime()
	formatted := rt.FormatThinkingLevels("anthropic", "claude-opus-4.6", ", ")
	if formatted == "" {
		t.Error("expected non-empty formatted levels")
	}
}
