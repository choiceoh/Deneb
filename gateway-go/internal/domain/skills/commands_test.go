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
	used := map[string]struct{}{"github": {}}

	// First call should get _2 suffix.
	name := resolveUniqueSkillCommandName("github", used)
	if name != "github_2" {
		t.Errorf("got %q, want github_2", name)
	}

	// Unused name should pass through.
	name = resolveUniqueSkillCommandName("weather", used)
	if name != "weather" {
		t.Errorf("got %q, want weather", name)
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
		t.Fatalf("got %d, want 2 specs (internal excluded)", len(specs))
	}
	if specs[0].Name != "github" {
		t.Errorf("got %q, want github", specs[0].Name)
	}
	if specs[1].Name != "weather" {
		t.Errorf("got %q, want weather", specs[1].Name)
	}
}

func TestBuildSkillCommandSpecs_reserved(t *testing.T) {
	entries := []SkillEntry{
		{
			Skill:      Skill{Name: "help", Description: "Help"},
			Invocation: &SkillInvocationPolicy{UserInvocable: true},
		},
	}
	reserved := map[string]struct{}{"help": {}}
	specs := BuildSkillCommandSpecs(entries, reserved)
	if len(specs) != 1 {
		t.Fatalf("got %d, want 1 spec", len(specs))
	}
	if specs[0].Name != "help_2" {
		t.Errorf("got %q, want help_2 (reserved name dedup)", specs[0].Name)
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
		t.Fatalf("got %d, want 1 spec", len(specs))
	}
	if specs[0].Dispatch == nil {
		t.Fatal("expected dispatch to be set")
	}
	if specs[0].Dispatch.ToolName != "my_tool" {
		t.Errorf("got %q, want toolName 'my_tool'", specs[0].Dispatch.ToolName)
	}
	if specs[0].Dispatch.ArgMode != "raw" {
		t.Errorf("got %q, want argMode 'raw'", specs[0].Dispatch.ArgMode)
	}
}
