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

func TestAllowedTools_SelfReview(t *testing.T) {
	allowed := AllowedTools(PresetSelfReview)
	if allowed == nil {
		t.Fatal("self-review preset should return non-nil allowed set")
	}
	for _, name := range []string{"fetch_tools", "skills", "skill_lifecycle"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("self-review preset should include %q", name)
		}
	}
	for _, name := range []string{
		"read", "write", "edit", "exec", "git", "web", "wiki", "kv",
		"message_send", "heartbeat_update", "sessions_spawn", "cron",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("self-review preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Business(t *testing.T) {
	allowed := AllowedTools(PresetBusiness)
	if allowed == nil {
		t.Fatal("business preset should return non-nil allowed set")
	}
	// Business preset covers the full Telegram-on-Android workflow.
	for _, name := range []string{
		"read", "write", "edit", "grep",
		"wiki", "graphify", "polaris",
		"gmail",
		"message", "clarify", "send_file",
		"cron", "heartbeat_update",
		"web",
		"sessions", "sessions_spawn", "subagents",
		"skills", "skill_lifecycle",
		"exec", "process",
		"gateway", "read_spillover", "fetch_tools",
		"kv",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("business preset should include %q", name)
		}
	}
	// `git` was removed from the project entirely in the coding-surface strip;
	// business preset must not silently revive it.
	if _, ok := allowed["git"]; ok {
		t.Error("business preset should NOT include git (tool no longer exists)")
	}
}

func TestIsValid(t *testing.T) {
	for _, p := range []Preset{PresetNone, PresetConversation, PresetBoot, PresetSelfReview, PresetBusiness} {
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
	if len(presets) != 4 {
		t.Errorf("got %d, want 4 known presets", len(presets))
	}
}
