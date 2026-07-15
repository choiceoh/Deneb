package genesis

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SelfCorrectionImpactDirectionDecrease = "decrease"
	SelfCorrectionImpactDirectionIncrease = "increase"

	SelfCorrectionImpactPending   = "pending"
	SelfCorrectionImpactVerified  = "verified"
	SelfCorrectionImpactNoEffect  = "no_effect"
	SelfCorrectionImpactRegressed = "regressed"
)

// SelfCorrectionImpactContract declares how a safely deployed L4 change will
// be judged for usefulness. It is advisory until delivery reaches
// watch_passed; the result is then classified deterministically by Go.
type SelfCorrectionImpactContract struct {
	Metric              string   `json:"metric"`
	Direction           string   `json:"direction"`
	Baseline            float64  `json:"baseline"`
	Target              float64  `json:"target"`
	MinSamples          int      `json:"minSamples"`
	ObservationWindowMs int64    `json:"observationWindowMs,omitempty"`
	Guardrails          []string `json:"guardrails,omitempty"`
}

// SelfCorrectionImpactResult is one terminal usefulness verdict for a
// watch-passed dispatch attempt. pending is derived and never persisted.
type SelfCorrectionImpactResult struct {
	Status              string   `json:"status"`
	Observed            float64  `json:"observed"`
	Samples             int      `json:"samples"`
	GuardrailViolations []string `json:"guardrailViolations,omitempty"`
	Note                string   `json:"note,omitempty"`
	CheckedAt           int64    `json:"checkedAt"`
}

// SelfCorrectionImpactStatus returns the independent usefulness axis. Safety
// still closes at watch_passed even when a legacy candidate has no contract.
func SelfCorrectionImpactStatus(record SelfCorrectionCandidateRecord) string {
	if record.ImpactResult != nil {
		return record.ImpactResult.Status
	}
	if record.ImpactContract != nil && record.DispatchPhase == selfCorrectionDispatchWatchPassed {
		return SelfCorrectionImpactPending
	}
	return ""
}

// RecordSelfCorrectionImpact appends a deterministic usefulness verdict for
// the exact dispatch attempt that survived the rollback watch.
func (t *Tracker) RecordSelfCorrectionImpact(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
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

func normalizeSelfCorrectionImpactContract(contract *SelfCorrectionImpactContract) (*SelfCorrectionImpactContract, error) {
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
	case SelfCorrectionImpactDirectionDecrease:
		if out.Target >= out.Baseline {
			return nil, fmt.Errorf("decrease target must be below baseline")
		}
	case SelfCorrectionImpactDirectionIncrease:
		if out.Target <= out.Baseline {
			return nil, fmt.Errorf("increase target must be above baseline")
		}
	default:
		return nil, fmt.Errorf("direction must be increase or decrease")
	}
	return &out, nil
}

func classifySelfCorrectionImpact(contract SelfCorrectionImpactContract, result SelfCorrectionImpactResult) (string, error) {
	if !finiteImpactValue(result.Observed) {
		return "", fmt.Errorf("observed value must be finite")
	}
	if result.Samples < contract.MinSamples {
		return "", fmt.Errorf("samples %d below minimum %d", result.Samples, contract.MinSamples)
	}
	if len(result.GuardrailViolations) > 0 {
		return SelfCorrectionImpactRegressed, nil
	}
	switch contract.Direction {
	case SelfCorrectionImpactDirectionDecrease:
		switch {
		case result.Observed <= contract.Target:
			return SelfCorrectionImpactVerified, nil
		case result.Observed > contract.Baseline:
			return SelfCorrectionImpactRegressed, nil
		default:
			return SelfCorrectionImpactNoEffect, nil
		}
	case SelfCorrectionImpactDirectionIncrease:
		switch {
		case result.Observed >= contract.Target:
			return SelfCorrectionImpactVerified, nil
		case result.Observed < contract.Baseline:
			return SelfCorrectionImpactRegressed, nil
		default:
			return SelfCorrectionImpactNoEffect, nil
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

func sameSelfCorrectionImpactResult(left, right SelfCorrectionImpactResult) bool {
	return left.Status == right.Status &&
		left.Observed == right.Observed &&
		left.Samples == right.Samples &&
		slices.Equal(left.GuardrailViolations, right.GuardrailViolations) &&
		left.Note == right.Note
}

func finiteImpactValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
