package toolpreset

import "testing"

func TestAllowedTools_Researcher(t *testing.T) {
	allowed := AllowedTools(PresetResearcher)
	if allowed == nil {
		t.Fatal("researcher preset should return non-nil allowed set")
	}
	// Researcher should have read-only tools.
	for _, name := range []string{"read", "grep", "find", "tree", "diff", "analyze", "web"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("researcher preset should include %q", name)
		}
	}
	// Researcher should NOT have write tools.
	for _, name := range []string{"write", "edit", "exec", "git", "multi_edit"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("researcher preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Implementer(t *testing.T) {
	allowed := AllowedTools(PresetImplementer)
	if allowed == nil {
		t.Fatal("implementer preset should return non-nil allowed set")
	}
	for _, name := range []string{"read", "write", "edit", "multi_edit", "exec", "test", "git"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("implementer preset should include %q", name)
		}
	}
	// Implementer should NOT have session/orchestration tools.
	for _, name := range []string{"sessions_spawn", "subagents"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("implementer preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Verifier(t *testing.T) {
	allowed := AllowedTools(PresetVerifier)
	if allowed == nil {
		t.Fatal("verifier preset should return non-nil allowed set")
	}
	for _, name := range []string{"read", "test", "exec", "diff"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("verifier preset should include %q", name)
		}
	}
	// Verifier should NOT have write tools.
	for _, name := range []string{"write", "edit", "git"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("verifier preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Coordinator(t *testing.T) {
	allowed := AllowedTools(PresetCoordinator)
	if allowed == nil {
		t.Fatal("coordinator preset should return non-nil allowed set")
	}
	for _, name := range []string{"sessions_spawn", "subagents", "sessions", "read", "grep", "find"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("coordinator preset should include %q", name)
		}
	}
	// Coordinator should NOT have write/exec tools.
	for _, name := range []string{"write", "edit", "exec", "git", "test"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("coordinator preset should NOT include %q", name)
		}
	}
}




func TestIsValid(t *testing.T) {
	for _, p := range []Preset{PresetNone, PresetResearcher, PresetImplementer, PresetVerifier, PresetCoordinator, PresetConversation} {
		if !IsValid(p) {
			t.Errorf("IsValid(%q) should be true", p)
		}
	}
	if IsValid("invalid") {
		t.Error("IsValid(\"invalid\") should be false")
	}
}

func TestKnownPresets(t *testing.T) {
	presets := KnownPresets()
	if len(presets) != 6 {
		t.Errorf("got %d, want 6 known presets", len(presets))
	}
}
