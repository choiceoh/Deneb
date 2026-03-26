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
