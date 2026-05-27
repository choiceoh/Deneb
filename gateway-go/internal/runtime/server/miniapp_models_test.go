package server

import "testing"

func TestBuildMiniappModelHealth(t *testing.T) {
	sections := []modelSection{{
		title: "models",
		entries: []modelEntry{
			{provider: "vllm", fullID: "vllm/qwen3.6-35b-a3b", display: "qwen3.6-35b-a3b"},
			{provider: "vllm", fullID: "vllm/missing-model", display: "missing-model"},
			{provider: "openrouter", fullID: "openrouter/anthropic/claude-sonnet-4.6", display: "claude-sonnet-4.6"},
			{provider: "zai", fullID: "zai/glm-5.1", display: "glm-5.1"},
		},
	}}
	probes := map[string]providerModelProbe{
		"vllm": {
			checked: true,
			models:  []string{"qwen3.6-35b-a3b"},
		},
		"openrouter": {
			checked: true,
			models:  []string{"anthropic/claude-sonnet-4.6"},
		},
	}

	got := buildMiniappModelHealth(sections, probes)
	want := map[string]string{
		"vllm/qwen3.6-35b-a3b":                   miniappModelHealthOnline,
		"vllm/missing-model":                     miniappModelHealthOffline,
		"openrouter/anthropic/claude-sonnet-4.6": miniappModelHealthOnline,
		"zai/glm-5.1":                            miniappModelHealthUnknown,
	}
	for modelID, status := range want {
		if got[modelID] != status {
			t.Errorf("health[%q] = %q, want %q", modelID, got[modelID], status)
		}
	}
}

func TestModelIDForProviderEntryKeepsNestedModelNames(t *testing.T) {
	entry := modelEntry{
		provider: "openrouter",
		fullID:   "openrouter/anthropic/claude-sonnet-4.6",
		display:  "claude-sonnet-4.6",
	}

	if got := modelIDForProviderEntry(entry); got != "anthropic/claude-sonnet-4.6" {
		t.Fatalf("modelIDForProviderEntry() = %q, want nested model id", got)
	}
}
