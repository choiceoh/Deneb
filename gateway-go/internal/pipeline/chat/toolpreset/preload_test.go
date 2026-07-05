package toolpreset

import "testing"

func TestPreloadedDeferredTools(t *testing.T) {
	// The self-review preset pre-loads its required deferred tools so the
	// review model can call them directly instead of doing a fetch_tools dance:
	// skill_lifecycle (the mandatory propose) and skills (the existing-skill
	// check the lean review prompt instructs).
	got := PreloadedDeferredTools(PresetSelfReview)
	if len(got) != 2 || got[0] != "skill_lifecycle" || got[1] != "skills" {
		t.Fatalf("self-review preload = %v, want [skill_lifecycle skills]", got)
	}

	// Every other preset (and the empty/main-chat preset) keeps the normal
	// fetch-on-demand behavior — no pre-load, so main chat's toolset/cache is
	// untouched.
	for _, p := range []Preset{Preset(""), PresetImplementer, PresetResearcher, PresetVerifier, PresetConversation} {
		if got := PreloadedDeferredTools(p); len(got) != 0 {
			t.Errorf("preload(%q) = %v, want nil (only self-review pre-loads)", p, got)
		}
	}
}
