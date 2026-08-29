package chat

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestRegisterCoreToolsCreatesExpectedToolSet(t *testing.T) {
	registry := NewToolRegistry()
	deps := &CoreToolDeps{
		WorkspaceDir: "/tmp/test-workspace",
		// fleet and browser register only when their host integration is
		// configured (2026-08-30); this contract is about the tool set, not the gate.
		Fleet:   tooldeps.FleetDeps{BaseURL: func() string { return "http://127.0.0.1:2" }},
		Browser: tooldeps.BrowserDeps{BaseURL: func() string { return "http://127.0.0.1:1" }},
	}
	RegisterCoreTools(registry, deps)
	if got := registry.ToolProvenanceRoot(); got != deps.WorkspaceDir {
		t.Fatalf("ToolProvenanceRoot() = %q, want %q", got, deps.WorkspaceDir)
	}

	// Verify expected tools are registered.
	expectedTools := []string{
		"read", "write", "edit", "grep",
		"exec", "process", "web",
		"message",
		"cron", "gateway", "observe", "fleet", "heartbeat_update",
		"sessions", "sessions_spawn",
		"fetch_tools",
		"blackboard",
		// Standing preference surface — must stay registered even without a wiki store.
		"preference",
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

func TestRegisterCoreToolsIncludesPreferenceAndWikiForget(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	registry := NewToolRegistry()
	RegisterCoreTools(registry, &CoreToolDeps{
		WorkspaceDir: dir,
		Wiki:         WikiDeps{Store: store},
	})

	registered := make(map[string]struct{})
	for _, name := range registry.Names() {
		registered[name] = struct{}{}
	}
	for _, name := range []string{"preference", "wiki_forget", "wiki"} {
		if _, ok := registered[name]; !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}
