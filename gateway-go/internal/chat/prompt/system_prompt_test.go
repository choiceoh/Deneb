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
			{Name: "memory_search"},
		},
		UserTimezone: "Asia/Seoul",
		RuntimeInfo: &RuntimeInfo{
			Host:         "dgx-spark",
			OS:           "linux",
			Arch:         "amd64",
			Model:        "claude-sonnet-4-20250514",
			DefaultModel: "claude-sonnet-4-20250514",
		},
		Channel: "telegram",
	}

	prompt := BuildSystemPrompt(params)

	// Check required sections exist.
	sections := []string{
		"You are Nev — a personal assistant running inside Deneb.",
		"## Communication",
		"## Attitude",
		"## How to Act",
		"## Trust and Respect",
		"## Tooling",
		"## Tool Usage",
		"## Memory Recall",
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
			{Name: "pilot"},
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
	if !strings.Contains(prompt, "AI: pilot") {
		t.Error("expected compact AI category")
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
	if !strings.Contains(prompt, "## Skills (mandatory)") {
		t.Error("missing skills section")
	}
	if !strings.Contains(prompt, "test-skill") {
		t.Error("missing skill content")
	}
}

func TestBuildSystemPromptNoSkills(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "## Skills") {
		t.Error("skills section should not appear when no skills prompt")
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
		Arch:    "amd64",
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
		t.Errorf("expected model claude-sonnet-4-6, got %s", info.Model)
	}
	if info.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("expected default_model claude-sonnet-4-6, got %s", info.DefaultModel)
	}
	if info.OS == "" {
		t.Error("expected OS to be set from runtime.GOOS")
	}
	if info.Arch == "" {
		t.Error("expected Arch to be set from runtime.GOARCH")
	}
}

func TestBuildSystemPromptPilotSection(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "pilot"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "pilot") {
		t.Error("expected pilot guidance in Tool Usage when pilot tool registered")
	}
	if !strings.Contains(prompt, "$ref") {
		t.Error("expected tool chaining info when pilot tool registered")
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
	if strings.Contains(prompt, "## Pilot & Chaining") {
		t.Error("Pilot section should not appear when pilot tool not registered")
	}
}

func TestBuildSystemPromptBlocksMatchesString(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "exec"},
			{Name: "pilot"},
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

func TestWriteCompactToolList_UncategorizedTools(t *testing.T) {
	toolSet := map[string]bool{
		"read":        true,
		"custom_tool": true,
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
