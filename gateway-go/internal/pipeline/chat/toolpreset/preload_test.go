package toolpreset

import "testing"

func TestPreloadedDeferredTools(t *testing.T) {
	// The self-review preset pre-loads its one required deferred tool so the
	// review model can call it directly instead of doing a fetch_tools dance.
	got := PreloadedDeferredTools(PresetSelfReview)
	if len(got) != 1 || got[0] != "skill_lifecycle" {
		t.Fatalf("self-review preload = %v, want [skill_lifecycle]", got)
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
