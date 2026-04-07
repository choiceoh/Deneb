package skills

import (
	"testing"
)

func TestSanitizeSkillCommandName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github", "github"},
		{"coding-agent", "coding_agent"},
		{"My Tool!", "my_tool"},
		{"", "skill"},
		{"___", "skill"},
		{"a-very-long-name-that-exceeds-the-max-length-for-commands", "a_very_long_name_that_exceeds_th"},
	}
	for _, tt := range tests {
		got := sanitizeSkillCommandName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeSkillCommandName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestResolveUniqueSkillCommandName(t *testing.T) {
	used := map[string]bool{"github": true}

	// First call should get _2 suffix.
	name := resolveUniqueSkillCommandName("github", used)
	if name != "github_2" {
		t.Errorf("expected github_2, got %q", name)
	}

	// Unused name should pass through.
	name = resolveUniqueSkillCommandName("weather", used)
	if name != "weather" {
		t.Errorf("expected weather, got %q", name)
	}
}

func TestBuildSkillCommandSpecs(t *testing.T) {
	entries := []SkillEntry{
		{
			Skill:      Skill{Name: "github", Description: "GitHub operations"},
			Invocation: &SkillInvocationPolicy{UserInvocable: true},
		},
		{
			Skill:      Skill{Name: "weather", Description: "Get weather info"},
			Invocation: &SkillInvocationPolicy{UserInvocable: true},
		},
		{
			Skill:      Skill{Name: "internal", Description: "Internal only"},
			Invocation: &SkillInvocationPolicy{UserInvocable: false},
		},
	}

	specs := BuildSkillCommandSpecs(entries, nil)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (internal excluded), got %d", len(specs))
	}
	if specs[0].Name != "github" {
		t.Errorf("expected github, got %q", specs[0].Name)
	}
	if specs[1].Name != "weather" {
		t.Errorf("expected weather, got %q", specs[1].Name)
	}
}

func TestBuildSkillCommandSpecs_reserved(t *testing.T) {
	entries := []SkillEntry{
		{
			Skill:      Skill{Name: "help", Description: "Help"},
			Invocation: &SkillInvocationPolicy{UserInvocable: true},
		},
	}
	reserved := map[string]bool{"help": true}
	specs := BuildSkillCommandSpecs(entries, reserved)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Name != "help_2" {
		t.Errorf("expected help_2 (reserved name dedup), got %q", specs[0].Name)
	}
}

func TestBuildSkillCommandSpecs_dispatch(t *testing.T) {
	entries := []SkillEntry{
		{
			Skill: Skill{Name: "run-tool", Description: "Run a tool"},
			Frontmatter: ParsedFrontmatter{
				"command-dispatch": "tool",
				"command-tool":     "my_tool",
				"command-arg-mode": "raw",
			},
			Invocation: &SkillInvocationPolicy{UserInvocable: true},
		},
	}

	specs := BuildSkillCommandSpecs(entries, nil)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Dispatch == nil {
		t.Fatal("expected dispatch to be set")
	}
	if specs[0].Dispatch.ToolName != "my_tool" {
		t.Errorf("expected toolName 'my_tool', got %q", specs[0].Dispatch.ToolName)
	}
	if specs[0].Dispatch.ArgMode != "raw" {
		t.Errorf("expected argMode 'raw', got %q", specs[0].Dispatch.ArgMode)
	}
}
