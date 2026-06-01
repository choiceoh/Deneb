package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

func TestBuildSystemPromptContainsSections(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/home/user/project",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "exec"},
			{Name: "wiki"},
		},
		UserTimezone: "Asia/Seoul",
		RuntimeInfo: &RuntimeInfo{
			Host:         "dgx-spark",
			OS:           "linux",
			Arch:         "arm64",
			Model:        "claude-sonnet-4-20250514",
			DefaultModel: "claude-sonnet-4-20250514",
		},
		Channel: "telegram",
	}

	prompt := BuildSystemPrompt(params)

	// Check required sections exist.
	sections := []string{
		"You are Nev — a personal assistant running inside Deneb (https://github.com/choiceoh/deneb).",
		"## 소통",
		"## 태도",
		"## 행동 원칙",
		"## 실행 우선",
		"## Trust and Respect",
		"## 안전",
		"## Tooling",
		"## Tool Usage",
		"## 위키 — 너의 외부 메모리",
		"## Messaging",
		"## Context",
		"/home/user/project",
		"Asia/Seoul",
		"host=dgx-spark",
		"channel=telegram",
	}

	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("system prompt missing section: %q", s)
		}
	}
}

func TestBuildSystemPromptHindsightSection(t *testing.T) {
	base := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
	}

	// Disabled: no Hindsight memory section.
	if got := BuildSystemPrompt(base); strings.Contains(got, "장기 기억 (Hindsight)") {
		t.Error("Hindsight section should be absent when HindsightEnabled is false")
	}

	// Enabled: the model is told it has a cross-session memory bank.
	base.HindsightEnabled = true
	got := BuildSystemPrompt(base)
	if !strings.Contains(got, "## 장기 기억 (Hindsight)") {
		t.Error("expected Hindsight memory section when HindsightEnabled is true")
	}
	if !strings.Contains(got, "<recall-context>") {
		t.Error("Hindsight section should reference the recall-context block")
	}
}

func TestBuildSystemPromptCompactToolList(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "write"},
			{Name: "exec"},
		},
	}

	prompt := BuildSystemPrompt(params)

	// Should contain categorized tool list format.
	if !strings.Contains(prompt, "File: read, write") {
		t.Error("expected compact File category with read, write")
	}
	if !strings.Contains(prompt, "Exec: exec") {
		t.Error("expected compact Exec category")
	}
	// Should NOT contain verbose per-tool descriptions in the tool list.
	if strings.Contains(prompt, "- read: Read files") {
		t.Error("expected compact list, not verbose per-tool descriptions")
	}
}

func TestBuildSystemPromptSkillsInjection(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		SkillsPrompt: `<available_skills><skill><name>test-skill</name></skill></available_skills>`,
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "## 스킬") {
		t.Error("missing skills section")
	}
	if !strings.Contains(prompt, "test-skill") {
		t.Error("missing skill content")
	}
	if !strings.Contains(prompt, "skills") {
		t.Error("missing skills tool hint for discoverable skills")
	}
}

func TestBuildSystemPromptNoSkills(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
	}

	prompt := BuildSystemPrompt(params)
	// Even without always-skills, the skills section appears with skills tool hint.
	if !strings.Contains(prompt, "## 스킬") {
		t.Error("skills section should always appear with skills tool hint")
	}
	if !strings.Contains(prompt, "skills") {
		t.Error("missing skills tool hint")
	}
}

func TestBuildSystemPromptNoMemoryWithoutTools(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "## Memory Recall") {
		t.Error("memory section should not appear without memory tools")
	}
}

func TestBuildRuntimeLine(t *testing.T) {
	info := &RuntimeInfo{
		AgentID: "default",
		Host:    "dgx-spark",
		OS:      "linux",
		Arch:    "arm64",
		Model:   "claude-sonnet-4-20250514",
	}

	line := buildRuntimeLine(info, "telegram")

	if !strings.Contains(line, "agent=default") {
		t.Error("missing agent ID")
	}
	if !strings.Contains(line, "host=dgx-spark") {
		t.Error("missing host")
	}
	if !strings.Contains(line, "channel=telegram") {
		t.Error("missing channel")
	}
}

func TestBuildRuntimeLine_NilInfo(t *testing.T) {
	line := buildRuntimeLine(nil, "telegram")
	if !strings.HasPrefix(line, "Runtime:") {
		t.Error("expected line to start with Runtime:")
	}
	if !strings.Contains(line, "channel=telegram") {
		t.Error("expected channel even with nil info")
	}
	// Should not contain any agent/host/os fields
	if strings.Contains(line, "agent=") || strings.Contains(line, "host=") {
		t.Error("expected no agent/host fields with nil info")
	}
}

func TestBuildRuntimeLine_NoChannel(t *testing.T) {
	info := &RuntimeInfo{
		Host: "my-host",
		OS:   "linux",
		Arch: "arm64",
	}
	line := buildRuntimeLine(info, "")
	if strings.Contains(line, "channel=") {
		t.Error("expected no channel field when empty")
	}
	if !strings.Contains(line, "host=my-host") {
		t.Error("missing host")
	}
	if !strings.Contains(line, "os=linux(arm64)") {
		t.Error("missing os(arch)")
	}
}

func TestBuildRuntimeLine_DefaultModel(t *testing.T) {
	info := &RuntimeInfo{
		Model:        "claude-opus-4-6",
		DefaultModel: "claude-sonnet-4-6",
	}
	line := buildRuntimeLine(info, "")
	if !strings.Contains(line, "model=claude-opus-4-6") {
		t.Error("missing model")
	}
	if !strings.Contains(line, "default_model=claude-sonnet-4-6") {
		t.Error("missing default_model")
	}
}

func TestBuildSystemPrompt_MessageToolSilentReply(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "message"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, SilentReplyToken) {
		t.Error("expected SilentReplyToken in messaging section when message tool is available")
	}
	if !strings.Contains(prompt, "외부 채널 전송이 실패하면 전달 상태는 실패/미확인이다.") {
		t.Error("expected explicit external-delivery failure guidance")
	}
}

func TestBuildSystemPrompt_NoMessageTool(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	// The message-specific guidance (proactive sends, NO_REPLY) should not appear without message tool
	if strings.Contains(prompt, "proactive sends") {
		t.Error("message-specific guidance should not appear without message tool")
	}
}

func TestBuildDefaultRuntimeInfo(t *testing.T) {
	info := BuildDefaultRuntimeInfo("claude-sonnet-4-6", "claude-sonnet-4-6")
	if info == nil {
		t.Fatal("expected non-nil RuntimeInfo")
	}
	if info.Model != "claude-sonnet-4-6" {
		t.Errorf("got %s, want model claude-sonnet-4-6", info.Model)
	}
	if info.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("got %s, want default_model claude-sonnet-4-6", info.DefaultModel)
	}
	if info.OS != "linux" {
		t.Errorf("got %q, want OS \"linux\"", info.OS)
	}
	if info.Arch == "" {
		t.Error("expected Arch to be set from runtime.GOARCH")
	}
}

func TestBuildSystemPromptNoPilotSection(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "pilot") {
		t.Error("pilot references should not appear in system prompt")
	}
}

// TestResolveTimezone_HonorsConfiguredZone is a regression guard for the
// timezone mismatch where resolveTimezone() read only the TZ env var and the
// server-local zone abbreviation, ignoring DENEB_TIMEZONE and the config
// "timezone" key. On a UTC container (the common deployment) that made the
// system prompt show UTC while logs, cron, and the calendar briefing — all
// dentime-based — ran in the configured zone (typically KST). resolveTimezone
// must now agree with pkg/dentime.
func TestResolveTimezone_HonorsConfiguredZone(t *testing.T) {
	t.Setenv("DENEB_TIMEZONE", "Asia/Seoul")
	dentime.ResetCache()
	t.Cleanup(dentime.ResetCache)

	if got := resolveTimezone(); got != "Asia/Seoul" {
		t.Fatalf("resolveTimezone() = %q, want %q (must defer to dentime, not server-local UTC)", got, "Asia/Seoul")
	}
}

// TestBuildSystemPromptDateInConfiguredZone verifies the rendered date line
// uses the configured zone, not the server-local zone. With an explicit
// UserTimezone the prompt must render "now" in that zone — proving the
// day-only date can flip a calendar day relative to UTC.
func TestBuildSystemPromptDateInConfiguredZone(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		UserTimezone: "Asia/Seoul",
	}
	prompt := BuildSystemPrompt(params)

	if !strings.Contains(prompt, "(timezone: Asia/Seoul)") {
		t.Fatalf("system prompt missing configured timezone label; got:\n%s", prompt)
	}
	wantDate := time.Now().In(time.FixedZone("KST", 9*60*60)).Format("Monday, January 2, 2006")
	if !strings.Contains(prompt, wantDate) {
		t.Errorf("system prompt date not rendered in Asia/Seoul; want %q in prompt", wantDate)
	}
}

func TestBuildSystemPromptBlocksMatchesString(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "exec"},
		},
		UserTimezone: "UTC",
	}

	stringPrompt := BuildSystemPrompt(params)
	blocks := BuildSystemPromptBlocks(params)

	// Blocks should concatenate to the same content as string version.
	var combined strings.Builder
	for _, b := range blocks {
		combined.WriteString(b.Text)
	}

	if combined.String() != stringPrompt {
		t.Error("BuildSystemPromptBlocks content should match BuildSystemPrompt")
	}
}

// TestBuildSystemPromptBlocks_CacheControlPlacement asserts the cache_control
// allocation: Static + Semi-static carry ephemeral markers; Dynamic does NOT.
// This invariant matters because Anthropic limits a request to 4 cache_control
// breakpoints. The 2 system markers leave room for 2 trailing message markers
// added by chat/buildTrailingCacheHook (Hermes Agent's "system_and_3" pattern).
// If the dynamic block ever regains a marker, trailing markers would push the
// request past the 4-breakpoint limit and the dynamic content (recall memory,
// timestamp, runtime info) would still cache-miss every turn.
func TestBuildSystemPromptBlocks_CacheControlPlacement(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>x</name></skill></available_skills>`,
	}
	blocks := BuildSystemPromptBlocks(params)
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks (static + dynamic), got %d", len(blocks))
	}
	if blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("static block missing ephemeral cache_control: %+v", blocks[0].CacheControl)
	}
	last := blocks[len(blocks)-1]
	if last.CacheControl != nil {
		t.Errorf("dynamic block must NOT carry cache_control (would consume a breakpoint without reuse)")
	}
	if len(blocks) == 3 {
		if blocks[1].CacheControl == nil || blocks[1].CacheControl.Type != "ephemeral" {
			t.Errorf("semi-static (skills) block missing ephemeral cache_control: %+v", blocks[1].CacheControl)
		}
	}
}

// TestBuildSystemPromptBlocks_CompactionFiredInjectsNote asserts the P4
// invariant: when CompactionFired=true, the dynamic block carries a one-
// time reminder that summaries are present in history. The reminder
// references the SUMMARY_PREFIX marker so the model bridges the two
// signals (system note + per-message prefix).
func TestBuildSystemPromptBlocks_CompactionFiredInjectsNote(t *testing.T) {
	base := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
	}

	noFlag := BuildSystemPromptBlocks(base)
	dynNoFlag := noFlag[len(noFlag)-1].Text
	if strings.Contains(dynNoFlag, "압축되었") {
		t.Errorf("compaction note must NOT appear when CompactionFired=false; dynamic=%q", dynNoFlag)
	}

	withFlag := base
	withFlag.CompactionFired = true
	flagged := BuildSystemPromptBlocks(withFlag)
	dynFlagged := flagged[len(flagged)-1].Text
	if !strings.Contains(dynFlagged, "압축되었") {
		t.Errorf("compaction note missing when CompactionFired=true; dynamic=%q", dynFlagged)
	}
	if !strings.Contains(dynFlagged, "[컨텍스트 요약 — 참고 전용]") {
		t.Errorf("compaction note must reference summary marker so the model bridges the two signals; dynamic=%q", dynFlagged)
	}
}

func TestBuildSystemPrompt_WikiSavingIsNotResponse(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "wiki"},
		},
	}

	prompt := BuildSystemPrompt(params)

	invariants := []string{
		"wiki write/log에 쓰는 내용은 사용자에게 보이지 않는다",
		"위키 저장은 응답이 아니다",
		"응답 텍스트에 직접 써라",
		"\"위키에 정리해뒀어\" / \"저장했어\" 만으로 응답을 끝내지 마라",
	}
	for _, s := range invariants {
		if !strings.Contains(prompt, s) {
			t.Errorf("wiki guidance missing invariant: %q", s)
		}
	}
}

func TestBuildSystemPromptConversationMode(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "web"},
			{Name: "wiki"},
		},
		ToolPreset: "conversation",
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "현재 모드: 대화") {
		t.Error("conversation mode block should appear when ToolPreset is 'conversation'")
	}
	if !strings.Contains(prompt, "대화와 리서치에 집중하는 모드") {
		t.Error("conversation mode should describe focus on dialogue and research")
	}
}

func TestBuildSystemPromptNormalModeNoConversationBlock(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "write"},
			{Name: "exec"},
			{Name: "web"},
			{Name: "wiki"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "현재 모드: 대화") {
		t.Error("conversation mode block should NOT appear in normal mode")
	}
}

func TestWriteCompactToolList_UncategorizedTools(t *testing.T) {
	toolSet := map[string]struct{}{
		"read":        {},
		"custom_tool": {},
	}

	var sb strings.Builder
	writeCompactToolList(&sb, toolSet)
	output := sb.String()

	if !strings.Contains(output, "File: read") {
		t.Error("expected categorized read in File group")
	}
	if !strings.Contains(output, "Other: custom_tool") {
		t.Error("expected uncategorized tool in Other group")
	}
}

func TestBuildSystemPrompt_WebToolGuidance(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "web"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "## Web") {
		t.Error("expected ## Web section when web tool is registered")
	}
	if !strings.Contains(prompt, "web(query=...)") {
		t.Error("expected web search guidance")
	}
	if !strings.Contains(prompt, "fetch failure") || !strings.Contains(prompt, "403") {
		t.Error("expected fetch failure guidance")
	}
}

func TestBuildSystemPrompt_NoWebGuidanceWithoutTools(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "## Web\n") {
		t.Error("web guidance should not appear without web/http tools")
	}
}
