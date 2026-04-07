package genesis

import (
	"testing"
	"time"
)

func TestSanitizeSkillName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Git Rebase Workflow", "git-rebase-workflow"},
		{"deploy_gateway", "deploy-gateway"},
		{"debug-ffi-crash", "debug-ffi-crash"},
		{"UPPER-CASE", "upper-case"},
		{"a", ""},   // too short
		{"", ""},    // empty
		{"---", ""}, // only hyphens
		{"hello!!world", "helloworld"},
		{"foo--bar", "foo-bar"},
		{"-leading-", "leading"},
	}
	for _, tt := range tests {
		got := sanitizeSkillName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSkillName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildToolSummary(t *testing.T) {
	activities := []ToolActivity{
		{Name: "read", IsError: false},
		{Name: "exec", IsError: false},
		{Name: "read", IsError: false},
		{Name: "write", IsError: false},
		{Name: "exec", IsError: true},
	}
	summary := buildToolSummary(activities)
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	// Should contain tool names.
	if !contains(summary, "read") || !contains(summary, "exec") || !contains(summary, "write") {
		t.Errorf("summary missing tool names: %s", summary)
	}
}

func TestEvaluate_MinToolCalls(t *testing.T) {
	svc := &Service{
		cfg: Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
	}
	// Too few tool calls.
	sctx := SessionContext{
		ToolActivities: make([]ToolActivity, 3),
		Turns:          5,
	}
	if svc.Evaluate(sctx) {
		t.Error("should reject session with too few tool calls")
	}
}

func TestEvaluate_MinTurns(t *testing.T) {
	svc := &Service{
		cfg: Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
	}
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"}, {Name: "exec"}, {Name: "write"},
			{Name: "read"}, {Name: "exec"},
		},
		Turns: 1,
	}
	if svc.Evaluate(sctx) {
		t.Error("should reject session with too few turns")
	}
}

func TestEvaluate_MinToolDiversity(t *testing.T) {
	svc := &Service{
		cfg:          Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
		recentSkills: make(map[string]time.Time),
	}
	// 5 calls but only 1 distinct tool.
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"}, {Name: "read"}, {Name: "read"},
			{Name: "read"}, {Name: "read"},
		},
		Turns: 5,
	}
	if svc.Evaluate(sctx) {
		t.Error("should reject session with low tool diversity")
	}
}

func TestEvaluate_Pass(t *testing.T) {
	svc := &Service{
		cfg:          Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
		recentSkills: make(map[string]time.Time),
	}
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"}, {Name: "exec"}, {Name: "write"},
			{Name: "grep"}, {Name: "read"},
		},
		Turns: 5,
	}
	if !svc.Evaluate(sctx) {
		t.Error("should accept session meeting all criteria")
	}
}

func TestBuildSkillMD(t *testing.T) {
	skill := &GeneratedSkill{
		Name:        "test-skill",
		Category:    "coding",
		Description: "A test skill for testing",
		Emoji:       "🧪",
		Tags:        []string{"test", "genesis"},
		Body:        "# Test Skill\n\nThis is a test.",
	}
	content := buildSkillMD("test-skill", skill)
	if !contains(content, "name: test-skill") {
		t.Error("missing name in frontmatter")
	}
	if !contains(content, "category: coding") {
		t.Error("missing category in frontmatter")
	}
	if !contains(content, `"origin":"genesis"`) {
		t.Error("missing genesis origin in metadata")
	}
	if !contains(content, "# Test Skill") {
		t.Error("missing body content")
	}
}

func TestBumpPatchVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"0.1.0", "0.1.1"},
		{"1.2.3", "1.2.4"},
		{"0.0.0", "0.0.1"},
		{"bad", "0.1.1"},
	}
	for _, tt := range tests {
		got := bumpPatchVersion(tt.input)
		if got != tt.want {
			t.Errorf("bumpPatchVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
