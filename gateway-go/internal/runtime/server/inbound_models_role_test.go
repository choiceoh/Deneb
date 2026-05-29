package server

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// newRoleTestRegistry builds a registry whose three roles are all non-vllm so
// construction stays hermetic (reconcileVllmModel is a no-op off vllm and fires
// no network probe — important on a host that actually runs a local vLLM).
func newRoleTestRegistry(t *testing.T) *modelrole.Registry {
	t.Helper()
	return modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:        "zai/glm-5.1",
		LightweightModel: "zai/glm-5-air",
		FallbackModel:    "zai/glm-5-air",
	})
}

func TestRegistryRoleEntries_NoLiveMainUsesRegistry(t *testing.T) {
	reg := newRoleTestRegistry(t)

	entries := registryRoleEntries(reg, "")
	if len(entries) == 0 {
		t.Fatal("expected role entries from the registry")
	}
	main := entries[0]
	if main.fullID != "zai/glm-5.1" {
		t.Errorf("main fullID = %q, want registry main zai/glm-5.1", main.fullID)
	}
	if main.label != "main: glm-5.1" {
		t.Errorf("main label = %q, want %q", main.label, "main: glm-5.1")
	}
}

// TestRegistryRoleEntries_LiveMainOverridesRegistry reproduces the picker bug:
// after a same-session main switch the chat-handler default moves to a model the
// registry was never told about. The 역할 section must follow that live default,
// and the stale registry main must not survive as a duplicate "main: <old>" row.
func TestRegistryRoleEntries_LiveMainOverridesRegistry(t *testing.T) {
	reg := newRoleTestRegistry(t)

	const live = "openrouter/anthropic/claude-sonnet-4.6"
	entries := registryRoleEntries(reg, live)
	if len(entries) == 0 {
		t.Fatal("expected role entries")
	}

	main := entries[0]
	if main.fullID != live {
		t.Errorf("main fullID = %q, want live default %q", main.fullID, live)
	}
	if main.provider != "openrouter" {
		t.Errorf("main provider = %q, want openrouter", main.provider)
	}
	// shortModelName strips every prefix up to the last slash.
	if main.display != "claude-sonnet-4.6" {
		t.Errorf("main display = %q, want claude-sonnet-4.6", main.display)
	}
	if main.label != "main: claude-sonnet-4.6" {
		t.Errorf("main label = %q, want %q", main.label, "main: claude-sonnet-4.6")
	}

	// The stale registry main must not linger anywhere in the role section.
	for _, e := range entries {
		if e.fullID == "zai/glm-5.1" {
			t.Errorf("stale registry main zai/glm-5.1 still present in role entries: %+v", entries)
		}
	}

	// The override only swaps the main row — lightweight/fallback are untouched,
	// so the row count matches the no-override case.
	if base := registryRoleEntries(reg, ""); len(entries) != len(base) {
		t.Errorf("override changed role-row count: got %d, base %d", len(entries), len(base))
	}
}

// TestRegistryRoleEntries_LiveMainDedupedFromProviderSection is the end-to-end
// guard: once the role section carries the live main, assembleMiniappModelSections
// must dedupe that model out of its provider section, leaving exactly one row for
// it (in 역할) rather than a stale role row plus a duplicate provider row.
func TestRegistryRoleEntries_LiveMainDedupedFromProviderSection(t *testing.T) {
	reg := newRoleTestRegistry(t)

	const live = "openrouter/anthropic/claude-sonnet-4.6"
	roles := registryRoleEntries(reg, live)
	providers := []providerSpec{
		{name: "openrouter", models: []string{"anthropic/claude-sonnet-4.6", "google/gemini-3.1-pro"}},
	}

	sections := assembleMiniappModelSections(roles, providers)
	if len(sections) == 0 || sections[0].title != "역할" {
		t.Fatalf("expected a leading 역할 section, got %+v", sections)
	}

	// The live main appears once, in the role section, and nowhere else.
	count := 0
	for _, sec := range sections {
		for _, e := range sec.entries {
			if e.fullID == live {
				count++
				if sec.title != "역할" {
					t.Errorf("live main surfaced in %q section, want only 역할", sec.title)
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("live main appears %d times across sections, want exactly 1", count)
	}
}
