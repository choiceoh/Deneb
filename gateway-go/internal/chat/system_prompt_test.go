package chat

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptContainsSections(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/home/user/project",
		ToolDefs: []ToolDef{
			{Name: "read", Description: "Read file contents"},
			{Name: "exec", Description: "Run shell commands"},
			{Name: "memory_search", Description: "Semantic memory search"},
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
		"You are a personal assistant running inside Deneb.",
		"## Tooling",
		"## Tool Call Style",
		"## Safety",
		"## Memory Recall",
		"## Workspace",
		"/home/user/project",
		"## Reply Tags",
		"## Messaging",
		"## Current Date & Time",
		"Asia/Seoul",
		"## Silent Replies",
		"NO_REPLY",
		"## Runtime",
		"host=dgx-spark",
		"channel=telegram",
		"## Deneb CLI Quick Reference",
	}

	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("system prompt missing section: %q", s)
		}
	}
}

func TestBuildSystemPromptToolOrder(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "exec", Description: "Run commands"},
			{Name: "read", Description: "Read files"},
			{Name: "write", Description: "Write files"},
		},
	}

	prompt := BuildSystemPrompt(params)

	// read should appear before exec in the prompt (per toolOrder).
	readIdx := strings.Index(prompt, "- read:")
	execIdx := strings.Index(prompt, "- exec:")
	if readIdx < 0 || execIdx < 0 {
		t.Fatal("missing read or exec in prompt")
	}
	if readIdx > execIdx {
		t.Error("read should appear before exec in tool list")
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
			{Name: "read", Description: "Read files"},
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
		OS:   "darwin",
		Arch: "arm64",
	}
	line := buildRuntimeLine(info, "")
	if strings.Contains(line, "channel=") {
		t.Error("expected no channel field when empty")
	}
	if !strings.Contains(line, "host=my-host") {
		t.Error("missing host")
	}
	if !strings.Contains(line, "os=darwin(arm64)") {
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

func TestWriteToolList_OrderAndDedup(t *testing.T) {
	defs := []ToolDef{
		{Name: "custom_tool", Description: "A custom tool"},
		{Name: "exec", Description: "Run commands"},
		{Name: "read", Description: "Read files"},
		{Name: "write", Description: "Write files"},
	}

	var sb strings.Builder
	writeToolList(&sb, defs)
	output := sb.String()

	// Ordered tools should appear first in preferred order
	readIdx := strings.Index(output, "- read:")
	writeIdx := strings.Index(output, "- write:")
	execIdx := strings.Index(output, "- exec:")
	customIdx := strings.Index(output, "- custom_tool:")

	if readIdx < 0 || writeIdx < 0 || execIdx < 0 || customIdx < 0 {
		t.Fatalf("missing tools in output: %s", output)
	}

	// read < write < exec (per toolOrder)
	if readIdx > writeIdx {
		t.Error("read should appear before write")
	}
	if writeIdx > execIdx {
		t.Error("write should appear before exec")
	}

	// custom_tool is not in toolOrder, so it comes after ordered tools
	if customIdx < execIdx {
		t.Error("custom_tool should appear after ordered tools")
	}
}

func TestWriteToolList_CoreSummaryOverride(t *testing.T) {
	defs := []ToolDef{
		{Name: "read", Description: "Generic description"},
	}

	var sb strings.Builder
	writeToolList(&sb, defs)
	output := sb.String()

	// Should use the coreToolSummaries description instead of the generic one
	if strings.Contains(output, "Generic description") {
		t.Error("expected core summary to override generic description")
	}
	if !strings.Contains(output, "Read file contents") {
		t.Errorf("expected core summary 'Read file contents', got: %s", output)
	}
}

func TestBuildSystemPrompt_MessageToolSilentReply(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "message", Description: "Send messages"},
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
			{Name: "read", Description: "Read files"},
		},
	}

	prompt := BuildSystemPrompt(params)
	// The silent reply section always appears, but the message-specific guidance should not
	if strings.Contains(prompt, "duplicate replies") {
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
