package chat

import (
	"testing"
)

func TestRegisterCoreTools(t *testing.T) {
	registry := NewToolRegistry()
	deps := &CoreToolDeps{
		WorkspaceDir: "/tmp/test-workspace",
	}
	RegisterCoreTools(registry, deps)

	// Verify expected tools are registered.
	expectedTools := []string{
		"read", "write", "edit", "grep", "find",
		"exec", "process", "web",
		"memory", "message",
		"cron", "gateway",
		"sessions_list", "sessions_history", "sessions_search",
		"sessions_send", "sessions_spawn",
		"subagents", "image", "youtube_transcript",
		"fetch_tools",
	}

	registered := make(map[string]bool)
	for _, name := range registry.Names() {
		registered[name] = true
	}
	for _, name := range expectedTools {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	// Verify total count.
	defs := registry.Definitions()
	if len(defs) < len(expectedTools) {
		t.Errorf("registered %d tools, expected at least %d", len(defs), len(expectedTools))
	}
}
