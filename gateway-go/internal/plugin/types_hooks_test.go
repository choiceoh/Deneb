package plugin

import (
	"testing"
)

func TestIsPluginHookName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"before_model_resolve", true},
		{"gateway_start", true},
		{"gateway_stop", true},
		{"session_start", true},
		{"session_end", true},
		{"subagent_spawning", true},
		{"unknown_hook", false},
		{"", false},
		{"before_model_resolve_extra", false},
	}
	for _, tt := range tests {
		got := IsPluginHookName(tt.name)
		if got != tt.want {
			t.Errorf("IsPluginHookName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsPromptInjectionHookName(t *testing.T) {
	tests := []struct {
		name HookName
		want bool
	}{
		{HookBeforePromptBuild, true},
		{HookBeforeAgentStart, true},
		{HookGatewayStart, false},
		{HookMessageSending, false},
		{HookLLMInput, false},
	}
	for _, tt := range tests {
		got := IsPromptInjectionHookName(tt.name)
		if got != tt.want {
			t.Errorf("IsPromptInjectionHookName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAllPluginHookNamesCompleteness(t *testing.T) {
	// Verify all defined constants are in AllPluginHookNames.
	knownHooks := map[HookName]bool{
		HookBeforeModelResolve:     true,
		HookBeforePromptBuild:      true,
		HookBeforeAgentStart:       true,
		HookLLMInput:               true,
		HookLLMOutput:              true,
		HookAgentEnd:               true,
		HookBeforeCompaction:       true,
		HookAfterCompaction:        true,
		HookBeforeReset:            true,
		HookInboundClaim:           true,
		HookMessageReceived:        true,
		HookMessageSending:         true,
		HookMessageSent:            true,
		HookBeforeToolCall:         true,
		HookAfterToolCall:          true,
		HookToolResultPersist:      true,
		HookBeforeMessageWrite:     true,
		HookSessionStart:           true,
		HookSessionEnd:             true,
		HookSubagentSpawning:       true,
		HookSubagentDeliveryTarget: true,
		HookSubagentSpawned:        true,
		HookSubagentEnded:          true,
		HookGatewayStart:           true,
		HookGatewayStop:            true,
	}

	for _, name := range AllPluginHookNames {
		if !knownHooks[name] {
			t.Errorf("AllPluginHookNames contains unknown hook: %q", name)
		}
		delete(knownHooks, name)
	}
	for name := range knownHooks {
		t.Errorf("hook %q not in AllPluginHookNames", name)
	}
}

func TestStripPromptMutationFieldsFromLegacyResult(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{"nil input", nil, nil},
		{"empty input", map[string]any{}, nil},
		{"only mutation fields", map[string]any{
			"systemPrompt":         "test",
			"prependContext":       "ctx",
			"prependSystemContext": "sys",
			"appendSystemContext":  "append",
		}, nil},
		{"mixed fields", map[string]any{
			"systemPrompt":    "test",
			"modelOverride":   "gpt-4",
			"providerOverride": "openai",
		}, map[string]any{
			"modelOverride":   "gpt-4",
			"providerOverride": "openai",
		}},
		{"only override fields", map[string]any{
			"modelOverride": "claude-3",
		}, map[string]any{
			"modelOverride": "claude-3",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripPromptMutationFieldsFromLegacyResult(tt.input)
			if tt.expect == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected %v, got nil", tt.expect)
			}
			for k, v := range tt.expect {
				if result[k] != v {
					t.Errorf("result[%q] = %v, want %v", k, result[k], v)
				}
			}
			if len(result) != len(tt.expect) {
				t.Errorf("result has %d keys, want %d", len(result), len(tt.expect))
			}
		})
	}
}

func TestPromptMutationResultFields(t *testing.T) {
	expected := map[string]bool{
		"systemPrompt":         true,
		"prependContext":       true,
		"prependSystemContext": true,
		"appendSystemContext":  true,
	}
	if len(PromptMutationResultFields) != len(expected) {
		t.Errorf("PromptMutationResultFields has %d entries, want %d",
			len(PromptMutationResultFields), len(expected))
	}
	for _, f := range PromptMutationResultFields {
		if !expected[f] {
			t.Errorf("unexpected field %q in PromptMutationResultFields", f)
		}
	}
}
