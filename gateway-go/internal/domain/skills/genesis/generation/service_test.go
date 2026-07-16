package generation

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
)

func TestSanitizeSkillNameLowercasesHyphenatesAndRejectsTooShort(t *testing.T) {
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
		got := genesiscommon.SanitizeSkillName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSkillName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildToolSummaryReturnsAllToolNames(t *testing.T) {
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

func TestDefaultConfigReturnsLowToolCallAndTurnThresholds(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MinToolCalls != 2 || cfg.MinTurns != 2 {
		t.Fatalf("unexpected low-attempt generation defaults: %+v", cfg)
	}
}

func TestEvaluateRejectsSessionWithTooFewToolCalls(t *testing.T) {
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

func TestEvaluateRejectsSessionWithTooFewTurns(t *testing.T) {
	svc := &Service{
		cfg: Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
	}
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"},
			{Name: "exec"},
			{Name: "write"},
			{Name: "read"},
			{Name: "exec"},
		},
		Turns: 1,
	}
	if svc.Evaluate(sctx) {
		t.Error("should reject session with too few turns")
	}
}

func TestEvaluateRejectsSessionWithLowToolDiversity(t *testing.T) {
	svc := &Service{
		cfg:          Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
		recentSkills: make(map[string]time.Time),
	}
	// 5 calls but only 1 distinct tool.
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"},
			{Name: "read"},
			{Name: "read"},
			{Name: "read"},
			{Name: "read"},
		},
		Turns: 5,
	}
	if svc.Evaluate(sctx) {
		t.Error("should reject session with low tool diversity")
	}
}

func TestEvaluateAllowsSessionMeetingAllCriteria(t *testing.T) {
	svc := &Service{
		cfg:          Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
		recentSkills: make(map[string]time.Time),
	}
	sctx := SessionContext{
		ToolActivities: []ToolActivity{
			{Name: "read"},
			{Name: "exec"},
			{Name: "write"},
			{Name: "grep"},
			{Name: "read"},
		},
		Turns: 5,
	}
	if !svc.Evaluate(sctx) {
		t.Error("should accept session meeting all criteria")
	}
}

func TestEvaluateReview_AllowsNarrowButReviewWorthySignal(t *testing.T) {
	svc := &Service{
		cfg: Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
	}
	sctx := SessionContext{
		ToolActivities: []ToolActivity{{Name: "exec"}, {Name: "exec"}},
		Turns:          1,
	}
	if !svc.EvaluateReview(sctx) {
		t.Fatal("review should run for narrow repeated tool activity even when genesis diversity gate would reject")
	}
	if svc.Evaluate(sctx) {
		t.Fatal("genesis Evaluate should remain stricter than review EvaluateReview")
	}
}

func TestEvaluateReviewRejectsSingleTrivialToolObservation(t *testing.T) {
	svc := &Service{
		cfg: Config{MinToolCalls: 5, MinTurns: 3, MaxSkillsPerDay: 10},
	}
	if svc.EvaluateReview(SessionContext{ToolActivities: []ToolActivity{{Name: "read"}}, Turns: 1}) {
		t.Fatal("review should reject a single trivial tool observation")
	}
}

func TestSkillSpecificityIssuesPassesWellFormedAndRejectsVagueSkills(t *testing.T) {
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
	for _, want := range []string{"너무 짧음", "When to Use", "Procedure", "Verification", "실행 지시", "트리거"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected issue mentioning %q, got: %s", want, joined)
		}
	}
}

func TestHasActionableInstructionSupportsOrderedAndHighFreedomContracts(t *testing.T) {
	if !hasActionableInstruction("1. 먼저 빌드한다\n2. 배포한다") {
		t.Error("numbered steps should count as actionable")
	}
	if !hasActionableInstruction("실행: `make go` 로 빌드") {
		t.Error("inline code should count as actionable")
	}
	if !hasActionableInstruction("- supplied transcript를 결정의 근거로 사용한다\n- 누락된 날짜는 추측하지 않고 질문으로 남긴다") {
		t.Error("two concrete decision criteria should support a high-freedom task")
	}
	if hasActionableInstruction("- 신중히 판단한다\n- 잘 처리한다") {
		t.Error("pure prose must not count as actionable")
	}
}

func TestSkillSpecificityIssuesAcceptsOutcomeDrivenProcedure(t *testing.T) {
	skill := &GeneratedSkill{
		Name:        "decision-brief",
		Description: "의사결정 브리프를 작성한다. Use when: 여러 근거에서 결론과 후속 조치를 정리할 때.",
		Body: "# 의사결정 브리프\n\n## When to Use\n회의 기록과 계획 문서에서 결정을 정리해야 할 때 사용한다. 단순 축약에는 사용하지 않는다.\n\n" +
			"## Procedure\n기대 결과는 의사결정자가 바로 승인 여부를 판단할 수 있는 브리프다.\n" +
			"- 회의 기록은 결정의 근거로, 계획 문서는 날짜와 예산의 근거로 사용한다.\n" +
			"- 제공되지 않은 사실은 추측하지 않고 공개 질문으로 남긴다.\n" +
			"결론에는 선택지별 영향과 권고안을 넣되, 자료에 없는 배경 설명은 추가하지 않는다. 대상 독자가 한 번 읽고 승인하거나 질문을 돌려보낼 수 있는 밀도로 작성한다.\n\n" +
			"## Pitfalls\n배경 설명이 결론을 가리지 않게 한다. 수치와 날짜를 바꾸지 않는다.\n\n" +
			"## Verification\n- 각 결정에는 담당자 또는 공개 질문이 연결되어 있다.\n" +
			"- 모든 날짜와 수치는 제공된 근거와 정확히 일치한다.",
	}
	if issues := skillSpecificityIssues(skill); len(issues) != 0 {
		t.Fatalf("outcome-driven high-freedom skill should pass, got issues: %v", issues)
	}
}

func TestBuildSkillMDReturnsFrontmatterOriginAndBody(t *testing.T) {
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

// TestJudgeGeneratedPassesThroughWhenNoJudgeWired verifies the genesis judge is fail-
// open: with no judge wired it falls through to the heuristic gate (prior
// behavior) instead of blocking all skill creation.
func TestJudgeGeneratedPassesThroughWhenNoJudgeWired(t *testing.T) {
	svc := &Service{logger: slog.Default()}
	pass, _ := svc.judgeGenerated(context.Background(), &GeneratedSkill{Name: "x", Body: "body"})
	if !pass {
		t.Fatal("no judge wired must pass through (fail-open)")
	}
}

// TestListExistingSkillDescriptionsReturnsNamesAndDescriptions verifies the judge's redundancy context
// includes existing skill names AND descriptions (token-Jaccard dedup can only
// see names; semantic duplicates need the descriptions).
func TestListExistingSkillDescriptionsReturnsNamesAndDescriptions(t *testing.T) {
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
