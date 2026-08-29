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
	// Context-gathering surfaces: mail_archive (eager) plus deferred ones
	// (contacts/graphify) that must be named to pass fetch_tools + Execute.
	for _, name := range []string{
		"read", "grep", "read_spillover", "web",
		"wiki", "knowledge", "polaris",
		"mail_archive", "contacts", "graphify", "blackboard", "fetch_tools",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("researcher preset should include %q", name)
		}
	}
	// No shell, no file writes, no escalation surfaces.
	for _, name := range []string{
		"write", "edit", "exec", "process",
		"message", "send_file", "cron", "gateway",
		"sessions_spawn", "subagents", "sessions", "skills",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("researcher preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_WikiScout(t *testing.T) {
	allowed := AllowedTools(PresetWikiScout)
	if allowed == nil {
		t.Fatal("wiki-scout preset should return non-nil allowed set")
	}
	// web is the job; wiki carries the three permitted writes (자료 ingest /
	// 로그 op / 미해결 질문 불릿 제거) plus wiki reads.
	for _, name := range []string{"web", "wiki", "fetch_tools"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("wiki-scout preset should include %q", name)
		}
	}
	// The scout's context carries fetched untrusted web pages and the
	// background path has no untrusted-origin tool gate — personal-memory
	// surfaces and file reads must be unreachable from the same turn so a
	// prompt-injected page cannot steer internal reads or leak them.
	for _, name := range []string{
		"mail_archive", "contacts", "graphify", "polaris", "knowledge",
		"read", "grep", "read_spillover",
		"write", "edit", "exec", "process",
		"message", "send_file", "sessions_spawn",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("wiki-scout preset must NOT include %q", name)
		}
	}
}

func TestAllowedTools_NotiDigest(t *testing.T) {
	allowed := AllowedTools(PresetNotiDigest)
	if allowed == nil {
		t.Fatal("noti-digest preset should return non-nil allowed set")
	}
	// wiki-only: read to find the project, write the 로그 op / person update.
	for _, name := range []string{"wiki", "fetch_tools"} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("noti-digest preset should include %q", name)
		}
	}
	// Batch content is raw third-party app text — no web channel and NO
	// personal-memory store may be reachable, or a malicious notification
	// could read private data and persist it through a wiki write.
	for _, name := range []string{
		"web",
		"mail_archive", "contacts", "graphify", "polaris", "knowledge",
		"read", "grep", "read_spillover",
		"write", "edit", "exec", "process",
		"message", "send_file", "sessions_spawn",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("noti-digest preset must NOT include %q", name)
		}
	}
}

func TestAllowedTools_WikiResearch(t *testing.T) {
	allowed := AllowedTools(PresetWikiResearch)
	if allowed == nil {
		t.Fatal("wiki-research preset should return non-nil allowed set")
	}
	// Internal context-gathering surfaces (the researcher set minus web).
	for _, name := range []string{
		"read", "grep", "read_spillover",
		"wiki", "knowledge", "polaris",
		"mail_archive", "contacts", "graphify", "fetch_tools",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("wiki-research preset should include %q", name)
		}
	}
	// Web is the whole point of this preset's divergence from researcher: the
	// autonomous wiki refresh is internal deep research, never external lookup.
	if _, ok := allowed["web"]; ok {
		t.Error("wiki-research preset must NOT include web (internal sources only)")
	}
	// No shell, writes-to-disk, escalation, or user-messaging surfaces.
	for _, name := range []string{
		"write", "edit", "exec", "process",
		"message", "send_file", "cron", "gateway",
		"sessions_spawn", "subagents",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("wiki-research preset should NOT include %q", name)
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
	// ...plus the symbol graph. The allow-list gates the deferred listing,
	// fetch_tools activation AND Execute, so a codegraph tool missing here is
	// invisible and uncallable — which is how the editing lane came to make
	// zero codegraph calls despite the server being configured.
	for _, name := range []string{
		"codegraph_impact", "codegraph_callers", "codegraph_callees",
		"codegraph_node", "codegraph_search", "codegraph_explore",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("implementer preset should include %q", name)
		}
	}
	// The symbol graph belongs to the lane that edits code, not to the
	// autonomous internal-research presets derived from researcher.
	for _, preset := range []Preset{PresetResearcher, PresetWikiResearch, PresetWikiScout, PresetNotiDigest} {
		if _, ok := AllowedTools(preset)["codegraph_impact"]; ok {
			t.Errorf("%q should not carry codegraph tools", preset)
		}
	}
	for _, name := range []string{"message", "send_file", "cron", "gateway", "sessions_spawn", "subagents"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("implementer preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Coding(t *testing.T) {
	allowed := AllowedTools(PresetCoding)
	if allowed == nil {
		t.Fatal("coding preset should return non-nil allowed set")
	}
	// Worktree coding surface: inspect + mutate + shell + web docs lookup.
	for _, name := range []string{
		"read", "grep", "read_spillover",
		"write", "edit", "exec", "process",
		"web", "blackboard", "fetch_tools",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("coding preset should include %q", name)
		}
	}
	// The whole point vs implementer: no 업무 memory / personal-data surfaces
	// inside an external repo worktree, and no spawn (children would not
	// inherit the worktree binding).
	for _, name := range []string{
		"mail_archive", "contacts", "graphify", "wiki", "knowledge", "polaris",
		"message", "send_file", "cron", "gateway", "calendar", "skills",
		"sessions_spawn", "subagents", "sessions",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("coding preset should NOT include %q", name)
		}
	}
}

func TestAllowedTools_Briefcase(t *testing.T) {
	allowed := AllowedTools(PresetBriefcase)
	if allowed == nil {
		t.Fatal("briefcase preset should return non-nil allowed set")
	}
	for _, name := range []string{
		"mail_archive", "contacts", "files", "calendar", "todo",
		"phone_read", "phone_write", "wiki", "knowledge", "polaris", "notebook",
		"read", "grep", "write", "edit",
	} {
		if _, ok := allowed[name]; !ok {
			t.Errorf("briefcase preset should include %q", name)
		}
	}
	for _, name := range []string{
		"web", "exec", "process", "message", "send_file", "cron", "gateway",
		"fleet", "sessions", "sessions_spawn", "subagents", "skills", "watch", "fetch_tools",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("briefcase preset should NOT include %q", name)
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
		"write", "edit", "web", "wiki", "knowledge",
		"message", "send_file", "cron", "gateway", "sessions_spawn", "subagents",
	} {
		if _, ok := allowed[name]; ok {
			t.Errorf("verifier preset should NOT include %q", name)
		}
	}
}

// TestSpawnPresetsDenySessionsSpawnAndSubagents pins the sandbox invariant: no
// spawn preset may grant sessions_spawn/subagents, because a restricted child
// spawning a preset-less (= unrestricted) grandchild would defeat the restriction.
func TestSpawnPresetsDenySessionsSpawnAndSubagents(t *testing.T) {
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

func TestIsValidAcceptsKnownPresetsRejectsUnknown(t *testing.T) {
	for _, p := range []Preset{
		PresetNone, PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier, PresetWikiResearch,
		PresetWikiScout, PresetNotiDigest, PresetCoding, PresetBriefcase, PresetProjection,
	} {
		if !IsValid(p) {
			t.Errorf("IsValid(%q) should be true", p)
		}
	}
	if IsValid("invalid") {
		t.Error("IsValid(\"invalid\") should be false")
	}
}

func TestKnownPresetsReturnsValidPresetsWithAllowLists(t *testing.T) {
	presets := KnownPresets()
	if len(presets) != 12 {
		t.Errorf("got %d, want 12 known presets", len(presets))
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

func TestProjectionPresetIsRecognizedAndDeniesEveryTool(t *testing.T) {
	allowed := AllowedTools(PresetProjection)
	if allowed == nil {
		t.Fatal("projection preset must be a recognized non-nil allow-list")
	}
	for _, name := range []string{"read", "web", "wiki", "fetch_tools", "message", "exec"} {
		if _, ok := allowed[name]; ok {
			t.Errorf("projection preset must deny %q", name)
		}
	}
}

func TestSpawnPresetsAreNotMissingFromKnownPresets(t *testing.T) {
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
