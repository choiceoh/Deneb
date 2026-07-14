package genesis

import (
	"context"
	"strings"
	"testing"
)

// Canned genesis responses for the fake executor. The clean body is padded
// well past the 400-rune specificity floor so it carries ZERO gate issues.
var (
	genCleanResp = `{"skip": false, "skill": {"name": "mail-to-wiki-sync", "category": "productivity",
		"description": "발주 메일을 위키에 반영. Use when: 발주 메일 정리. NOT for: 열람.",
		"body": "# 동기화\n\n## When to Use\n발주 메일이 쌓였을 때 실행한다.\n\n## Procedure\n1. 메일을 검색한다.\n2. 위키에 반영한다.\n3. 재조회로 검증한다.\n\n## Pitfalls\n중복 페이지 주의.\n\n## Verification\n재조회. ` + strings.Repeat("상세 절차 설명. ", 60) + `"}}`
	genVagueResp = `{"skip": false, "skill": {"name": "be-careful", "category": "productivity",
		"description": "신중하게",
		"body": "맥락을 잘 살펴서 신중하게 작업하라."}}`
	genSkipResp = `{"skip": true, "reason": "일회성 세션"}`
)

// cannedGen returns incumbent/proposal responses per call side, keyed by the
// system prompt marker the test passes.
func cannedGen(incResp, propResp string) genesisShadowGenFn {
	return func(_ context.Context, systemPrompt, _ string) (string, error) {
		if systemPrompt == "INCUMBENT" {
			return incResp, nil
		}
		return propResp, nil
	}
}

// The genesis shadow bench compares both prompts on identical fixtures with
// the production admissibility gate: a proposal that degrades or skips a
// scenario the incumbent handles cleanly flips and rejects; matching quality
// clears the bench (adoption confidence is the router's job, not the gate's).
func TestGenesisShadowBenchFlipsOnDegradationOrSkipClearsOnMatchingQuality(t *testing.T) {
	scenarios := genesisShadowScenarios()
	if len(scenarios) < 3 {
		t.Fatalf("expected >=3 compiled fixtures, got %d", len(scenarios))
	}

	// Proposal degrades every scenario to a vague skill → flips, hard reject.
	out := runGenesisShadowBench(context.Background(), "INCUMBENT", "PROPOSAL", scenarios, cannedGen(genCleanResp, genVagueResp))
	if out.Flips != len(scenarios) || out.Scenarios != len(scenarios) {
		t.Fatalf("degrading proposal: flips=%d scenarios=%d, want all flipped: %+v", out.Flips, out.Scenarios, out)
	}
	if reason := genesisBenchDecision(out); !strings.Contains(reason, "flipped") {
		t.Fatalf("flips must reject: %q", reason)
	}

	// Proposal skips every skill-worthy scenario → flips even though no
	// scenario enters the scored set (capability loss must not hide in skips).
	out = runGenesisShadowBench(context.Background(), "INCUMBENT", "PROPOSAL", scenarios, cannedGen(genCleanResp, genSkipResp))
	if out.Flips != len(scenarios) || out.Scenarios != 0 || out.ProposalSkips != len(scenarios) {
		t.Fatalf("skipping proposal: %+v", out)
	}
	if reason := genesisBenchDecision(out); !strings.Contains(reason, "flipped") {
		t.Fatalf("skip-flips must reject: %q", reason)
	}

	// Matching clean quality clears the bench (decision "") — and reads as
	// low-confidence (0→0 margin) so adoption routes to the operator verdict.
	out = runGenesisShadowBench(context.Background(), "INCUMBENT", "PROPOSAL", scenarios, cannedGen(genCleanResp, genCleanResp))
	if out.Flips != 0 || out.Scenarios != len(scenarios) {
		t.Fatalf("clean pair: %+v", out)
	}
	if reason := genesisBenchDecision(out); reason != "" {
		t.Fatalf("clean pair must clear the bench: %q", reason)
	}
	if lc := metaLowConfidenceReason(nil, nil, nil, &out); !strings.Contains(lc, "no measurable improvement") {
		t.Fatalf("0→0 margin must route to operator verdict: %q", lc)
	}

	// Both sides unparsable → zero scored scenarios, bench clears, and the
	// low-confidence router (not auto-adoption) owns the outcome.
	out = runGenesisShadowBench(context.Background(), "INCUMBENT", "PROPOSAL", scenarios, cannedGen("산문 응답", "산문 응답"))
	if out.Scenarios != 0 || out.Flips != 0 {
		t.Fatalf("unparsable pair: %+v", out)
	}
	if reason := genesisBenchDecision(out); reason != "" {
		t.Fatalf("zero-scenario bench must defer to routing: %q", reason)
	}
	if lc := metaLowConfidenceReason(nil, nil, nil, &out); !strings.Contains(lc, "no scenario") {
		t.Fatalf("zero-scenario must read low-confidence: %q", lc)
	}
}

// A vague incumbent is not a flip when the proposal is equally vague — flips
// key on CLEAN incumbent scenarios only; the mean-issue regression rule
// handles graded movement.
func TestGenesisBenchDecisionRejectsMeanIssueRegressionNotEqualVagueness(t *testing.T) {
	scenarios := genesisShadowScenarios()[:1]
	out := runGenesisShadowBench(context.Background(), "INCUMBENT", "PROPOSAL", scenarios, cannedGen(genVagueResp, genVagueResp))
	if out.Flips != 0 || out.Scenarios != 1 {
		t.Fatalf("equally vague pair must not flip: %+v", out)
	}
	if reason := genesisBenchDecision(out); reason != "" {
		t.Fatalf("no regression between equally vague outputs: %q", reason)
	}
	out.ProposalIssues = out.IncumbentIssues + 1.0
	if reason := genesisBenchDecision(out); !strings.Contains(reason, "regressed") {
		t.Fatalf("mean-issue regression must reject: %q", reason)
	}
}
