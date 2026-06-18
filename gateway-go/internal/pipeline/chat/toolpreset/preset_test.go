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

func TestAllowedTools_Researcher(t *testing.T) {
	allowed := AllowedTools(PresetResearcher)
	if allowed == nil {
		t.Fatal("researcher preset should return non-nil allowed set")
	}
	// Context-gathering surfaces, including deferred ones (mail_archive/contacts/
	// graphify) that must be named to pass fetch_tools + Execute.
	for _, name := range []string{
		"read", "grep", "read_spillover", "web",
		"wiki", "knowledge", "polaris",
		"mail_archive", "contacts", "graphify", "fetch_tools",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("researcher preset should include %q", name)
		}
	}
	// No shell, no file writes, no escalation surfaces.
	for _, name := range []string{
		"write", "edit", "exec", "process",
		"message", "send_file", "cron", "gateway",
		"sessions_spawn", "subagents", "sessions", "skills", "gmail",
	} {
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
	// Strict superset of researcher...
	for name := range AllowedTools(PresetResearcher) {
		if _, ok := allowed[name]; !ok {
			t.Errorf("implementer preset should include researcher tool %q", name)
		}
	}
	// ...plus mutation + shell.
	for _, name := range []string{"write", "edit", "exec", "process"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("implementer preset should include %q", name)
		}
	}
	for _, name := range []string{"message", "send_file", "cron", "gateway", "sessions_spawn", "subagents"} {
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
	for _, name := range []string{"read", "grep", "read_spillover", "exec", "process", "fetch_tools"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("verifier preset should include %q", name)
		}
	}
	// No write surface (a verifier that patches what it judges defeats the
	// role) and no research/messaging surfaces.
	for _, name := range []string{
		"write", "edit", "web", "gmail", "wiki", "knowledge",
		"message", "send_file", "cron", "gateway", "sessions_spawn", "subagents",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("verifier preset should NOT include %q", name)
		}
	}
}

// TestSpawnPresets_CannotSpawn pins the sandbox invariant: no spawn preset may
// grant sessions_spawn/subagents, because a restricted child spawning a
// preset-less (= unrestricted) grandchild would defeat the restriction.
func TestSpawnPresets_CannotSpawn(t *testing.T) {
	for _, p := range SpawnPresets() {
		allowed := AllowedTools(p)
		if allowed == nil {
			t.Fatalf("spawn preset %q must have an allow-list", p)
		}
		for _, name := range []string{"sessions_spawn", "subagents"} {
			if _, ok := allowed[name]; ok {
				t.Errorf("spawn preset %q must NOT include %q", p, name)
			}
		}
	}
}

func TestIsValid(t *testing.T) {
	for _, p := range []Preset{
		PresetNone, PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier,
	} {
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
	for _, p := range presets {
		if AllowedTools(p) == nil {
			t.Errorf("known preset %q has no allow-list (AllowedTools returned nil)", p)
		}
		if !IsValid(p) {
			t.Errorf("known preset %q should be valid", p)
		}
	}
}

func TestSpawnPresets_AreKnown(t *testing.T) {
	known := make(map[Preset]struct{})
	for _, p := range KnownPresets() {
		known[p] = struct{}{}
	}
	for _, p := range SpawnPresets() {
		if _, ok := known[p]; !ok {
			t.Errorf("spawn preset %q missing from KnownPresets", p)
		}
	}
}
