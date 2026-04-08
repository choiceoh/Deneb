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
		"message",
		"cron", "gateway",
		"sessions", "sessions_spawn",
		"subagents", "youtube_transcript",
		"fetch_tools",
	}

	registered := make(map[string]struct{})
	for _, name := range registry.Names() {
		registered[name] = struct{}{}
	}
	for _, name := range expectedTools {
		if _, ok := registered[name]; !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	// Verify total count.
	defs := registry.Definitions()
	if len(defs) < len(expectedTools) {
		t.Errorf("registered %d tools, expected at least %d", len(defs), len(expectedTools))
	}
}
