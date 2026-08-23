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
func TestRunProducerShadowBenchDetectsFlipsAndIgnoresSkipsAndErrors(t *testing.T) {
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
func TestProducerBenchDecisionRejectsFlipsAndRegressionAllowsNoise(t *testing.T) {
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
func TestShadowCandidateBodyEmptyOnSkipOrUnparsableJSON(t *testing.T) {
	if got := shadowCandidateBody(producerResp("the body")); got != "the body" {
		t.Fatalf("body = %q", got)
	}
	for _, junk := range []string{`{"skip": true}`, "not json", `{"skip": false}`} {
		if got := shadowCandidateBody(junk); got != "" {
			t.Fatalf("junk %q produced body %q", junk, got)
		}
	}
}

// The verdict must cite its own evidence: flip notes come first in the capped
// Notes and are the only notes a flip rejection quotes — the ledger carried
// "flipped 1 case(s): <skill>: one-sided skip/unparsable" verdicts whose cited
// note described an unrelated, unscored scenario. A scenario where NEITHER
// side produced a candidate is labelled as such, not "one-sided".
func TestRunProducerShadowBenchKeepsFlipNotesFirstAndLabelsBothSidesSkip(t *testing.T) {
	scenarios := []producerShadowScenario{
		{Skill: "both-empty", UserPrompt: "both", Cases: []SkillValidationCaseRecord{{ID: "c0", SkillName: "both-empty", RequiredSubstrings: []string{"x"}}}},
		{Skill: "one-sided", UserPrompt: "one", Cases: []SkillValidationCaseRecord{{ID: "c1", SkillName: "one-sided", RequiredSubstrings: []string{"x"}}}},
		{Skill: "err-a", UserPrompt: "err", Cases: []SkillValidationCaseRecord{{ID: "c2", SkillName: "err-a", RequiredSubstrings: []string{"x"}}}},
		{Skill: "err-b", UserPrompt: "err", Cases: []SkillValidationCaseRecord{{ID: "c3", SkillName: "err-b", RequiredSubstrings: []string{"x"}}}},
		shadowScenario("case-flip", "required step"),
	}
	genFn := func(_ context.Context, systemPrompt, userPrompt string) (string, error) {
		switch userPrompt {
		case "both":
			return "not json", nil
		case "one":
			if systemPrompt == "incumbent" {
				return producerResp("body"), nil
			}
			return `{"skip": true}`, nil
		case "err":
			return "", fmt.Errorf("boom")
		}
		if systemPrompt == "incumbent" {
			return producerResp("body with required step"), nil
		}
		return producerResp("body missing it"), nil
	}
	out := runProducerShadowBench(context.Background(), "incumbent", "proposal", scenarios, genFn)
	if out.Flips != 1 || out.Skills != 1 {
		t.Fatalf("bench = %+v", out)
	}
	if len(out.Notes) == 0 || !strings.Contains(out.Notes[0], "sk: flip on") {
		t.Fatalf("flip note must lead the capped notes: %v", out.Notes)
	}
	if joined := strings.Join(out.Notes, " | "); !strings.Contains(joined, "both-empty: both sides skip/unparsable") {
		t.Fatalf("both-sides label missing: %v", out.Notes)
	}
	if r := producerBenchDecision(out); !strings.Contains(r, "sk: flip on") || strings.Contains(r, "skip/unparsable") {
		t.Fatalf("decision must cite flip notes only: %q", r)
	}
}

// An unbenched producer proposal (no scenario scored) is low-confidence for the
// right reason — not a "measured" 0.00→0.00 margin.
func TestMetaLowConfidenceReasonNamesUnscoredShadowBench(t *testing.T) {
	if got := metaLowConfidenceReason(nil, nil, &producerBenchOutcome{Skills: 0}, nil); !strings.Contains(got, "scored no scenario") {
		t.Fatalf("reason = %q", got)
	}
	if got := metaLowConfidenceReason(nil, nil, &producerBenchOutcome{Skills: 2, IncumbentScore: 60, ProposalScore: 70}, nil); got != "" {
		t.Fatalf("improving margin must not be low-confidence: %q", got)
	}
}
