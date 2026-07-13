package genesis

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func shadowScenario(caseID, required string) producerShadowScenario {
	return producerShadowScenario{
		Skill:      "sk",
		UserPrompt: "## 현재 SKILL.md\n...",
		Cases:      []SkillValidationCaseRecord{{ID: caseID, SkillName: "sk", RequiredSubstrings: []string{required}}},
	}
}

func producerResp(body string) string {
	return fmt.Sprintf(`{"skip": false, "changes": {"description": "d", "new_version": "0.1.1", "body": %q}}`, body)
}

// Flip counting: proposal-generated candidate failing a case the incumbent's
// candidate passes is a flip; equal-or-better outcomes are not.
func TestRunProducerShadowBench(t *testing.T) {
	scenarios := []producerShadowScenario{shadowScenario("case-a", "required step")}

	gen := func(incBody, propBody string) producerShadowGenFn {
		return func(_ context.Context, systemPrompt, _ string) (string, error) {
			if systemPrompt == "incumbent" {
				return producerResp(incBody), nil
			}
			return producerResp(propBody), nil
		}
	}

	out := runProducerShadowBench(context.Background(), "incumbent", "proposal", scenarios,
		gen("body with required step", "body missing it"))
	if out.Skills != 1 || out.Flips != 1 {
		t.Fatalf("flip not detected: %+v", out)
	}

	out = runProducerShadowBench(context.Background(), "incumbent", "proposal", scenarios,
		gen("body with required step", "better body with required step"))
	if out.Skills != 1 || out.Flips != 0 || out.ProposalScore != 100 {
		t.Fatalf("clean replay misscored: %+v", out)
	}

	// One-sided skip: scenario contributes nothing (no flip, no score).
	skipGen := func(_ context.Context, systemPrompt, _ string) (string, error) {
		if systemPrompt == "incumbent" {
			return producerResp("body with required step"), nil
		}
		return `{"skip": true, "reason": "no improvement"}`, nil
	}
	out = runProducerShadowBench(context.Background(), "incumbent", "proposal", scenarios, skipGen)
	if out.Skills != 0 || out.Flips != 0 {
		t.Fatalf("one-sided skip must not count: %+v", out)
	}

	// Generation errors are noted, not fatal.
	out = runProducerShadowBench(context.Background(), "incumbent", "proposal", scenarios,
		func(_ context.Context, _, _ string) (string, error) { return "", fmt.Errorf("boom") })
	if out.Skills != 0 || len(out.Notes) == 0 {
		t.Fatalf("generation error unnoted: %+v", out)
	}
}

// Promotion rule: flips reject outright; mean regression beyond epsilon
// rejects; an unbenchable corpus (zero scenarios) stays propose-only ("").
func TestProducerBenchDecision(t *testing.T) {
	if r := producerBenchDecision(producerBenchOutcome{Skills: 0}); r != "" {
		t.Fatalf("unbenchable corpus must stay propose-only: %q", r)
	}
	if r := producerBenchDecision(producerBenchOutcome{Skills: 2, Flips: 1}); !strings.Contains(r, "flip") {
		t.Fatalf("flip not rejected: %q", r)
	}
	if r := producerBenchDecision(producerBenchOutcome{Skills: 2, IncumbentScore: 90, ProposalScore: 80}); !strings.Contains(r, "regressed") {
		t.Fatalf("mean regression not rejected: %q", r)
	}
	if r := producerBenchDecision(producerBenchOutcome{Skills: 2, IncumbentScore: 90, ProposalScore: 88}); r != "" {
		t.Fatalf("within-epsilon noise rejected: %q", r)
	}
}

// shadowCandidateBody must survive skip/junk without producing a body.
func TestShadowCandidateBody(t *testing.T) {
	if got := shadowCandidateBody(producerResp("the body")); got != "the body" {
		t.Fatalf("body = %q", got)
	}
	for _, junk := range []string{`{"skip": true}`, "not json", `{"skip": false}`} {
		if got := shadowCandidateBody(junk); got != "" {
			t.Fatalf("junk %q produced body %q", junk, got)
		}
	}
}
