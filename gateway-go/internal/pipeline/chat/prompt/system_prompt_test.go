package prompt

import (
	"strings"
	"testing"
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
	if !strings.Contains(prompt, "## Skills") {
		t.Error("missing skills section")
	}
	if !strings.Contains(prompt, "test-skill") {
		t.Error("missing skill content")
	}
	if !strings.Contains(prompt, "skills_list") {
		t.Error("missing skills_list tool hint for discoverable skills")
	}
}

func TestBuildSystemPromptNoSkills(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
	}

	prompt := BuildSystemPrompt(params)
	// Even without always-skills, the skills section appears with skills_list tool hint.
	if !strings.Contains(prompt, "## Skills") {
		t.Error("skills section should always appear with skills_list tool hint")
	}
	if !strings.Contains(prompt, "skills_list") {
		t.Error("missing skills_list tool hint")
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

func TestBuildSystemPromptConversationMode(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "web"},
			{Name: "http"},
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
			{Name: "http"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "## Web") {
		t.Error("expected ## Web section when web/http tools are registered")
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
