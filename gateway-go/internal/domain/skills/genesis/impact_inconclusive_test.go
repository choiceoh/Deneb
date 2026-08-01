package genesis

import (
	"strings"
	"testing"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
)

// Every impact status except this one is a CLAIM about the change. Without a
// verdict for "we looked and could not tell", an unmeasurable case has to become
// the nearest claim — the collapse this ledger kept reproducing (audit
// 2026-08-01: quiet stream read as fixed, baselined finding read as fixed,
// failed judge call read as rejected).

func increaseContract(minSamples int) rsilifecycle.ImpactContract {
	return rsilifecycle.ImpactContract{
		Direction: selfCorrectionImpactDirectionIncrease, Baseline: 0, Target: 48, MinSamples: minSamples,
	}
}

func TestInsufficientSamplesIsAVerdictNotAnError(t *testing.T) {
	// It used to raise an error, which the miner logged at Debug and dropped —
	// so the honest answer was the one answer the ledger could not hold.
	got, err := classifySelfCorrectionImpact(increaseContract(1),
		rsilifecycle.ImpactResult{Observed: 72, Samples: 0})
	if err != nil {
		t.Fatalf("no-evidence must not fail the record: %v", err)
	}
	if got != selfCorrectionImpactInconclusive {
		t.Errorf("want inconclusive, got %q", got)
	}
}

func TestInconclusiveBeatsTheTargetItWouldOtherwiseMeet(t *testing.T) {
	// Observed 72 clears the target of 48. With no samples behind it that
	// number is not evidence, and must not be scored as a verified fix.
	got, _ := classifySelfCorrectionImpact(increaseContract(1),
		rsilifecycle.ImpactResult{Observed: 72, Samples: 0})
	if got == selfCorrectionImpactVerified {
		t.Error("a target met with zero samples must not verify")
	}
}

func TestMeasuredResultsStillClassifyNormally(t *testing.T) {
	verified, _ := classifySelfCorrectionImpact(increaseContract(1),
		rsilifecycle.ImpactResult{Observed: 72, Samples: 1})
	if verified != selfCorrectionImpactVerified {
		t.Errorf("evidence-backed success must still verify, got %q", verified)
	}
	noEffect, _ := classifySelfCorrectionImpact(increaseContract(1),
		rsilifecycle.ImpactResult{Observed: 10, Samples: 1})
	if noEffect != selfCorrectionImpactNoEffect {
		t.Errorf("evidence-backed shortfall must still read no_effect, got %q", noEffect)
	}
}

func TestGuardrailViolationOutranksMissingEvidence(t *testing.T) {
	// A tripped guardrail is observed harm; it must not be softened to "could
	// not tell" just because the sample count is thin.
	got, _ := classifySelfCorrectionImpact(increaseContract(5),
		rsilifecycle.ImpactResult{Observed: 72, Samples: 0, GuardrailViolations: []string{"latency"}})
	if got != selfCorrectionImpactRegressed {
		t.Errorf("guardrail violations must still regress, got %q", got)
	}
}

// A metric satisfied by the observed thing DISAPPEARING can be satisfied by
// removing the signal instead of the defect — and the agent being scored is
// often the one who could remove it. Both 2026-08-01 instances were that shape.

func TestDisappearanceOracleMustDeclareAGuardrail(t *testing.T) {
	_, err := normalizeSelfCorrectionImpactContract(&rsilifecycle.ImpactContract{
		Metric: "deadcode.finding_present:abc", Direction: selfCorrectionImpactDirectionDecrease,
		Baseline: 1, Target: 0, MinSamples: 1,
	})
	if err == nil {
		t.Fatal("a decrease-to-zero oracle with no guardrail must be rejected")
	}
	if !strings.Contains(err.Error(), "guardrail") {
		t.Errorf("the error must say what is missing: %v", err)
	}
}

func TestDisappearanceOracleAcceptedWithAGuardrail(t *testing.T) {
	if _, err := normalizeSelfCorrectionImpactContract(&rsilifecycle.ImpactContract{
		Metric: "deadcode.finding_present:abc", Direction: selfCorrectionImpactDirectionDecrease,
		Baseline: 1, Target: 0, MinSamples: 1, Guardrails: []string{"not_baselined"},
	}); err != nil {
		t.Errorf("a declared falsifier must satisfy the rule: %v", err)
	}
}

func TestNonDisappearanceMetricsStayUnconstrained(t *testing.T) {
	// A magnitude that must improve is not satisfiable by deletion, so it needs
	// no guardrail to be honest.
	if _, err := normalizeSelfCorrectionImpactContract(&rsilifecycle.ImpactContract{
		Metric: "runtime.health.score:latency", Direction: selfCorrectionImpactDirectionIncrease,
		Baseline: 20, Target: 60, MinSamples: 1,
	}); err != nil {
		t.Errorf("an improvement metric must not require a guardrail: %v", err)
	}
}
