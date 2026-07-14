package genesis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func benchBody() string {
	return strings.TrimSpace(`# 테스트 스킬

## When to Use
업무 보고서를 생성할 때. ` + strings.Repeat("구체적인 사용 조건 설명. ", 10) + `

## Procedure
1. 데이터를 수집한다. ` + strings.Repeat("절차 상세. ", 10) + `
2. 보고서를 작성한다.

## Verification
결과를 검증한다. ` + strings.Repeat("검증 상세. ", 10))
}

// Each mechanical degradation must actually plant its defect — otherwise the
// bench measures nothing.
func TestDegradationFunctionsCreateTheirIntendedDefects(t *testing.T) {
	body := benchBody()

	dropped, ok := degradeDropLastSection(body)
	if !ok || strings.Contains(dropped, "## Verification") {
		t.Fatalf("section-drop kept the last section (ok=%v)", ok)
	}
	faked, ok := degradeFakeTool(body)
	if !ok || !strings.Contains(faked, "deneb-hyperfix") {
		t.Fatal("fake-tool did not inject the invented tool")
	}
	truncated, ok := degradeTruncate(body)
	if !ok || len([]rune(truncated)) >= len([]rune(body)) {
		t.Fatal("truncation did not shrink the body")
	}
	overfit, ok := degradeOverfit(body)
	if !ok || !strings.Contains(overfit, "PR #3448") {
		t.Fatal("overfit did not pin session identifiers")
	}
}

// Pair construction is deterministic over the catalog and skips stub bodies.
func TestBuildJudgeDegradationPairsIgnoresStubsAndIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) skills.SkillEntry {
		path := filepath.Join(dir, name+".md")
		full := "---\nname: " + name + "\nversion: 1.0.0\n---\n" + content
		if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
			t.Fatal(err)
		}
		e := skills.SkillEntry{}
		e.Skill.Name = name
		e.Skill.FilePath = path
		return e
	}
	entries := []skills.SkillEntry{
		write("real", benchBody()),
		write("stub", "# 짧은 스텁"),
	}

	pairs := buildJudgeDegradationPairs(entries, 10)
	if len(pairs) != 4 {
		t.Fatalf("pairs = %d, want 4 (all degradations of the one real body)", len(pairs))
	}
	for _, p := range pairs {
		if p.Skill != "real" {
			t.Fatalf("stub body produced a pair: %+v", p)
		}
		if p.Degraded == p.Original {
			t.Fatalf("degradation %s is a no-op", p.Degradation)
		}
	}
	if again := buildJudgeDegradationPairs(entries, 10); len(again) != len(pairs) || again[0].Degradation != pairs[0].Degradation {
		t.Fatal("pair construction is not deterministic")
	}
	if capped := buildJudgeDegradationPairs(entries, 2); len(capped) != 2 {
		t.Fatalf("limit not honored: %d", len(capped))
	}
}

// Bench scoring: correct = judge rejects the planted defect without scoring it
// above the original; passes, inverted scores, and errors are failures.
func TestRunJudgeDegradationBenchCountsInvertedScoresAndErrorsAsFailures(t *testing.T) {
	pairs := []judgeBenchPair{
		{Skill: "a", Degradation: "section-drop"},
		{Skill: "a", Degradation: "fake-tool"},
		{Skill: "a", Degradation: "truncation"},
		{Skill: "a", Degradation: "overfit"},
	}
	score := func(o, c float64) (op, cp *float64) { return &o, &c }

	verdicts := map[string]judgeVerdict{}
	verdicts["section-drop"] = judgeVerdict{Pass: false}                                      // correct (no scores)
	o1, c1 := score(80, 60)                                                                   //nolint:govet // clarity
	verdicts["fake-tool"] = judgeVerdict{Pass: false, OriginalScore: o1, CandidateScore: c1}  // correct
	o2, c2 := score(60, 80)                                                                   //nolint:govet
	verdicts["truncation"] = judgeVerdict{Pass: false, OriginalScore: o2, CandidateScore: c2} // inverted scores → failure
	verdicts["overfit"] = judgeVerdict{Pass: true}                                            // passed a defect → failure

	out := runJudgeDegradationBench(context.Background(), "prompt", pairs,
		func(_ context.Context, _, _, _ string) (judgeVerdict, error) { return judgeVerdict{}, nil })
	_ = out // exercise the trivially-rejecting judge below instead

	out = runJudgeDegradationBench(context.Background(), "prompt", pairs,
		func(_ context.Context, _, _, degraded string) (judgeVerdict, error) {
			_ = degraded
			return judgeVerdict{}, fmt.Errorf("boom")
		})
	if out.Correct != 0 || out.Total != 4 {
		t.Fatalf("verdict errors must count as failures: %+v", out)
	}

	i := -1
	out = runJudgeDegradationBench(context.Background(), "prompt", pairs,
		func(_ context.Context, _, _, _ string) (judgeVerdict, error) {
			i++
			return verdicts[pairs[i].Degradation], nil
		})
	if out.Correct != 2 || out.Total != 4 {
		t.Fatalf("bench = %d/%d, want 2/4: %v", out.Correct, out.Total, out.Failures)
	}
}

// Promotion rule: proposal must clear the floor AND not regress the incumbent.
func TestJudgeBenchDecisionRejectsRegressionFloorMissAndTinySample(t *testing.T) {
	mk := func(correct, total int) judgeBenchOutcome { return judgeBenchOutcome{Correct: correct, Total: total} }
	if r := judgeBenchDecision(mk(3, 6), mk(4, 6)); r != "" {
		t.Fatalf("improving proposal rejected: %s", r)
	}
	if r := judgeBenchDecision(mk(5, 6), mk(4, 6)); !strings.Contains(r, "regresses") {
		t.Fatalf("regressing proposal admitted: %q", r)
	}
	if r := judgeBenchDecision(mk(1, 6), mk(2, 6)); !strings.Contains(r, "floor") {
		t.Fatalf("below-floor proposal admitted: %q", r)
	}
	if r := judgeBenchDecision(mk(2, 2), mk(2, 2)); !strings.Contains(r, "too small") {
		t.Fatalf("unbenchable proposal admitted: %q", r)
	}
}
