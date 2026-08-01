package genesis

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	selfCorrectionImpactDirectionDecrease = "decrease"
	selfCorrectionImpactDirectionIncrease = "increase"

	selfCorrectionImpactPending   = "pending"
	selfCorrectionImpactVerified  = "verified"
	selfCorrectionImpactNoEffect  = "no_effect"
	selfCorrectionImpactRegressed = "regressed"
	// selfCorrectionImpactInconclusive is the verdict for "we looked and could
	// not tell" — the measurement ran but carried too little evidence to place
	// the result against the contract.
	//
	// It exists because every other outcome is a CLAIM about the fix, and
	// without this one an unmeasurable case has to become the nearest claim.
	// That collapse is the defect this whole ledger kept reproducing (audit
	// 2026-08-01): a quiet error stream read as "fixed", a baselined finding
	// read as "fixed", a failed judge call read as "rejected". Insufficient
	// samples used to raise an ERROR here, which the miner logged at Debug and
	// dropped — so the honest answer was the one answer the ledger could not
	// record. Unlike pending it states that a measurement was attempted, and
	// unlike no_effect it makes no claim about the change.
	selfCorrectionImpactInconclusive = "inconclusive"
)

// selfCorrectionImpactStatus returns the independent usefulness axis. Safety
// still closes at watch_passed even when a legacy candidate has no contract.
func selfCorrectionImpactStatus(record SelfCorrectionCandidateRecord) string {
	if record.ImpactResult != nil {
		return record.ImpactResult.Status
	}
	if record.ImpactContract != nil && record.DispatchPhase == selfCorrectionDispatchWatchPassed {
		return selfCorrectionImpactPending
	}
	return ""
}

// recordSelfCorrectionImpact appends a deterministic usefulness verdict for
// the exact dispatch attempt that survived the rollback watch.
func (t *Tracker) recordSelfCorrectionImpact(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.Type = selfCorrectionTypeImpact
	record.ID = strings.TrimSpace(record.ID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	if record.ID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction impact id is required")
	}
	if record.AttemptID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction impact attemptId is required")
	}
	if record.ImpactResult == nil {
		return record, fmt.Errorf("genesis-tracker: self-correction impact result is required")
	}

	current, found, err := t.mergedSelfCorrectionCandidateLocked(record.ID)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	if !found {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s", record.ID)
	}
	if current.DispatchPhase != selfCorrectionDispatchWatchPassed {
		return record, fmt.Errorf("genesis-tracker: impact requires watch_passed delivery, got %s", current.DispatchPhase)
	}
	if current.AttemptID != record.AttemptID {
		return record, fmt.Errorf("genesis-tracker: impact attempt mismatch: %s != %s", current.AttemptID, record.AttemptID)
	}
	contract, err := normalizeSelfCorrectionImpactContract(current.ImpactContract)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: invalid self-correction impact contract: %w", err)
	}
	if contract == nil {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate has no impact contract")
	}

	result := *record.ImpactResult
	result.Note = strings.TrimSpace(result.Note)
	result.GuardrailViolations = cleanSelfCorrectionStrings(result.GuardrailViolations, 0)
	for _, violation := range result.GuardrailViolations {
		if !slices.Contains(contract.Guardrails, violation) {
			return record, fmt.Errorf("genesis-tracker: undeclared impact guardrail violation %q", violation)
		}
	}
	status, err := classifySelfCorrectionImpact(*contract, result)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: classify self-correction impact: %w", err)
	}
	now := time.Now().UnixMilli()
	result.Status = status
	result.CheckedAt = now
	record.ImpactResult = &result
	record.CreatedAt = now
	record.UpdatedAt = now

	if current.ImpactResult != nil {
		if sameSelfCorrectionImpactResult(*current.ImpactResult, result) {
			return current, nil
		}
		return record, fmt.Errorf("genesis-tracker: self-correction impact result is already terminal: %s", current.ImpactResult.Status)
	}
	if elapsed := now - current.UpdatedAt; elapsed < contract.ObservationWindowMs {
		return record, fmt.Errorf(
			"genesis-tracker: impact observation window has not elapsed: %dms remaining",
			contract.ObservationWindowMs-elapsed,
		)
	}
	if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
		return record, fmt.Errorf("genesis-tracker: append self-correction impact: %w", err)
	}
	return record, nil
}

func normalizeSelfCorrectionImpactContract(contract *rsilifecycle.ImpactContract) (*rsilifecycle.ImpactContract, error) {
	if contract == nil {
		return nil, nil
	}
	out := *contract
	out.Metric = strings.TrimSpace(out.Metric)
	out.Direction = strings.ToLower(strings.TrimSpace(out.Direction))
	out.Guardrails = cleanSelfCorrectionStrings(out.Guardrails, 0)
	if out.Metric == "" {
		return nil, fmt.Errorf("metric is required")
	}
	if len([]rune(out.Metric)) > 160 {
		return nil, fmt.Errorf("metric exceeds 160 characters")
	}
	// A metric satisfied by the observed thing DISAPPEARING can be satisfied by
	// removing the signal instead of the defect, and the agent doing the fixing
	// is usually the one who could remove it. Both live instances were exactly
	// that shape (audit 2026-08-01): a deadcode finding left the audit by being
	// added to a baseline the dispatched agent can edit, and a runtime signature
	// went quiet with no check that anything still exercised the path.
	//
	// Guardrails are the declared falsifiers — the things whose movement means a
	// "success" is not one. Requiring at least one on a disappearance oracle
	// forces its author to say, up front, what would prove the absence hollow.
	// Other shapes (a magnitude that must improve, a score that must rise) are
	// not satisfiable by deletion and stay unconstrained.
	if out.Direction == selfCorrectionImpactDirectionDecrease && out.Target == 0 && len(out.Guardrails) == 0 {
		return nil, fmt.Errorf(
			"a decrease-to-zero metric must declare at least one guardrail: absence is satisfiable by suppressing the signal",
		)
	}
	if out.MinSamples <= 0 {
		return nil, fmt.Errorf("minSamples must be positive")
	}
	if out.ObservationWindowMs < 0 {
		return nil, fmt.Errorf("observationWindowMs cannot be negative")
	}
	if len(out.Guardrails) > 8 {
		return nil, fmt.Errorf("guardrails exceed maximum of 8")
	}
	if !finiteImpactValue(out.Baseline) || !finiteImpactValue(out.Target) {
		return nil, fmt.Errorf("baseline and target must be finite")
	}
	switch out.Direction {
	case selfCorrectionImpactDirectionDecrease:
		if out.Target >= out.Baseline {
			return nil, fmt.Errorf("decrease target must be below baseline")
		}
	case selfCorrectionImpactDirectionIncrease:
		if out.Target <= out.Baseline {
			return nil, fmt.Errorf("increase target must be above baseline")
		}
	default:
		return nil, fmt.Errorf("direction must be increase or decrease")
	}
	return &out, nil
}

func classifySelfCorrectionImpact(contract rsilifecycle.ImpactContract, result rsilifecycle.ImpactResult) (string, error) {
	if !finiteImpactValue(result.Observed) {
		return "", fmt.Errorf("observed value must be finite")
	}
	// Guardrails first: a tripped guardrail is OBSERVED harm, not an inference
	// from the primary metric, so a thin sample count must not soften it to
	// "could not tell".
	if len(result.GuardrailViolations) > 0 {
		return selfCorrectionImpactRegressed, nil
	}
	// Too little evidence is a verdict, not a failure. Returning an error here
	// made the caller drop the observation entirely, leaving the candidate
	// pending forever with nothing recording WHY it could never be judged.
	if result.Samples < contract.MinSamples {
		return selfCorrectionImpactInconclusive, nil
	}
	switch contract.Direction {
	case selfCorrectionImpactDirectionDecrease:
		switch {
		case result.Observed <= contract.Target:
			return selfCorrectionImpactVerified, nil
		case result.Observed > contract.Baseline:
			return selfCorrectionImpactRegressed, nil
		default:
			return selfCorrectionImpactNoEffect, nil
		}
	case selfCorrectionImpactDirectionIncrease:
		switch {
		case result.Observed >= contract.Target:
			return selfCorrectionImpactVerified, nil
		case result.Observed < contract.Baseline:
			return selfCorrectionImpactRegressed, nil
		default:
			return selfCorrectionImpactNoEffect, nil
		}
	default:
		return "", fmt.Errorf("unknown direction %q", contract.Direction)
	}
}

func applySelfCorrectionImpact(base, record SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	if record.ImpactResult != nil {
		result := *record.ImpactResult
		base.ImpactResult = &result
	}
	if record.UpdatedAt > 0 {
		base.UpdatedAt = record.UpdatedAt
	}
	return base
}

func sameSelfCorrectionImpactResult(left, right rsilifecycle.ImpactResult) bool {
	return left.Status == right.Status &&
		left.Observed == right.Observed &&
		left.Samples == right.Samples &&
		slices.Equal(left.GuardrailViolations, right.GuardrailViolations) &&
		left.Note == right.Note
}

func finiteImpactValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
