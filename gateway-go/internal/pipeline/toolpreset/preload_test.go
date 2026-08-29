package toolpreset

import "testing"

// TestPreloadedDeferredTools pins preset-owned deferred activation behavior.
func TestPreloadedDeferredTools(t *testing.T) {
	// The self-review preset pre-loads its required deferred tools so the
	// review model can call them directly instead of doing a fetch_tools dance:
	// skill_lifecycle (the mandatory propose) and skills (the existing-skill
	// check the lean review prompt instructs).
	got := PreloadedDeferredTools(PresetSelfReview)
	if len(got) != 2 || got[0] != "skill_lifecycle" || got[1] != "skills" {
		t.Fatalf("self-review preload = %v, want [skill_lifecycle skills]", got)
	}

	// The implementer preset pre-loads the two codegraph tools its editing
	// procedure names. A required first step must not sit behind a fetch_tools
	// round-trip the model routinely skips — the same reasoning as self-review.
	got = PreloadedDeferredTools(PresetImplementer)
	if len(got) != 2 || got[0] != "codegraph_impact" || got[1] != "codegraph_node" {
		t.Fatalf("implementer preload = %v, want [codegraph_impact codegraph_node]", got)
	}
	// Whatever is preloaded must also be allowed, or the preloaded tool is
	// advertised and then rejected at Execute.
	allowed := AllowedTools(PresetImplementer)
	for _, name := range got {
		if _, ok := allowed[name]; !ok {
			t.Errorf("preloaded %q is not in the implementer allow-list", name)
		}
	}

	// Every other preset (and the empty/main-chat preset) keeps the normal
	// fetch-on-demand behavior — no pre-load, so main chat's toolset/cache is
	// untouched.
	for _, p := range []Preset{Preset(""), PresetResearcher, PresetVerifier, PresetConversation, PresetBriefcase} {
		if got := PreloadedDeferredTools(p); len(got) != 0 {
			t.Errorf("preload(%q) = %v, want nil", p, got)
		}
	}
}
