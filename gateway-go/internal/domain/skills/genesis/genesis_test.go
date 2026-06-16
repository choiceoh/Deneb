package genesis

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
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

func TestDefaultConfigKeepsAttemptThresholdLow(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MinToolCalls != 2 || cfg.MinTurns != 2 || DefaultNudgeInterval != 3 || DefaultEvolveEventThreshold != 3 {
		t.Fatalf("unexpected low-attempt defaults: %+v nudge=%d evolveThreshold=%d", cfg, DefaultNudgeInterval, DefaultEvolveEventThreshold)
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

func TestSkillSpecificityIssues(t *testing.T) {
	good := &GeneratedSkill{
		Name:        "deploy-gateway",
		Description: "게이트웨이를 배포한다. Use when: 코드 머지 후 프로덕션 반영이 필요할 때. NOT for: 로컬 테스트.",
		Body: "# 게이트웨이 배포\n\n## When to Use\n" +
			"PR이 main에 머지되어 프로덕션 게이트웨이에 변경을 반영해야 할 때 사용한다. " +
			"단순 로컬 검증이나 dev 인스턴스 재시작에는 쓰지 않는다.\n\n" +
			"## Procedure\n1. `make gateway-prod` 로 프로덕션 바이너리를 빌드한다.\n" +
			"2. 워치독이 있으면 먼저 PAUSE 한다(재시작 부활 범위 안에서 트립 방지).\n" +
			"3. `scripts/deploy/deploy.sh` 를 실행해 SIGUSR1 핫리스타트를 건다.\n" +
			"4. `/health` 와 로그로 기동 단계를 확인한다.\n\n" +
			"## Pitfalls\n- 워치독을 먼저 PAUSE 하지 않으면 재시작이 트립된다.\n" +
			"- 컨텍스트 길이를 바꿨다면 launcher 와 deneb.json 양쪽을 동기화한다.\n\n" +
			"## Verification\n`ss -ltnp | rg 18789` 로 포트가 떴는지 확인하고, " +
			"로그에 에러/경고가 없는지 본다.",
	}
	if issues := skillSpecificityIssues(good); len(issues) != 0 {
		t.Fatalf("well-formed skill should pass, got issues: %v", issues)
	}

	// Vague prose, no sections, no steps, no trigger — the EvolveR failure mode.
	vague := &GeneratedSkill{
		Name:        "be-careful",
		Description: "유용한 일반 지침",
		Body:        "# 주의\n\n맥락을 잘 살펴보고 신중하게 작업하세요.",
	}
	issues := skillSpecificityIssues(vague)
	if len(issues) == 0 {
		t.Fatal("vague skill must be rejected")
	}
	joined := strings.Join(issues, "; ")
	for _, want := range []string{"너무 짧음", "When to Use", "Procedure", "단계", "트리거"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected issue mentioning %q, got: %s", want, joined)
		}
	}
}

func TestHasActionableStep(t *testing.T) {
	if !hasActionableStep("1. 먼저 빌드한다\n2. 배포한다") {
		t.Error("numbered steps should count as actionable")
	}
	if !hasActionableStep("실행: `make go` 로 빌드") {
		t.Error("inline code should count as actionable")
	}
	if hasActionableStep("맥락을 잘 살펴보세요. 신중하게 판단하세요.") {
		t.Error("pure prose must not count as actionable")
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

// TestJudgeGenerated_NoJudgePassesThrough verifies the genesis judge is fail-
// open: with no judge wired it falls through to the heuristic gate (prior
// behavior) instead of blocking all skill creation.
func TestJudgeGenerated_NoJudgePassesThrough(t *testing.T) {
	svc := &Service{logger: slog.Default()}
	pass, _ := svc.judgeGenerated(context.Background(), &GeneratedSkill{Name: "x", Body: "body"})
	if !pass {
		t.Fatal("no judge wired must pass through (fail-open)")
	}
}

// TestListExistingSkillDescriptions verifies the judge's redundancy context
// includes existing skill names AND descriptions (token-Jaccard dedup can only
// see names; semantic duplicates need the descriptions).
func TestListExistingSkillDescriptions(t *testing.T) {
	cat := skills.NewCatalog(nil)
	cat.Register(skills.SkillEntry{Skill: skills.Skill{Name: "morning-letter", Description: "wiki+gmail morning letter"}})
	cat.Register(skills.SkillEntry{Skill: skills.Skill{Name: "deploy", Description: "deploy gateway"}})
	svc := &Service{catalog: cat, logger: slog.Default()}
	out := svc.listExistingSkillDescriptions()
	for _, want := range []string{"morning-letter", "wiki+gmail", "deploy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("descriptions missing %q in: %q", want, out)
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
