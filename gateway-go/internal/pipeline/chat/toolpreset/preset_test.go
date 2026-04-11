package toolpreset

import "testing"

func TestAllowedTools_Conversation(t *testing.T) {
	allowed := AllowedTools(PresetConversation)
	if allowed == nil {
		t.Fatal("conversation preset should return non-nil allowed set")
	}
	for _, name := range []string{"read", "web", "wiki"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("conversation preset should include %q", name)
		}
	}
	for _, name := range []string{"write", "edit", "exec", "git"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("conversation preset should NOT include %q", name)
		}
	}
}

func TestIsValid(t *testing.T) {
	for _, p := range []Preset{PresetNone, PresetConversation, PresetBoot} {
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
	if len(presets) != 2 {
		t.Errorf("got %d, want 2 known presets", len(presets))
	}
}
