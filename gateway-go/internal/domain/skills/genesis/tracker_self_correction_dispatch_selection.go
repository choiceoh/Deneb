package genesis

import (
	"strings"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"
)

// NextSelfCorrectionDispatchCandidate folds the complete append-only ledger and
// chooses one candidate without sorting or imposing a recency cap. Review-
// approved work wins, then a source whose latest measured fix regressed or had
// no effect, then newest work. Two consecutive negative outcomes require a new
// proposed strategy before that source can consume another unattended session,
// and a candidate that has failed to land maxSelfCorrectionDispatchFailures
// times is withheld entirely (SelfCorrectionDispatchEligible) so an impossible
// fix stops consuming a coding session on every tick. Local marker/worktree
// residue remains an executor concern and is supplied as excluded IDs by the
// RPC client.
func (t *Tracker) NextSelfCorrectionDispatchCandidate(
	excludedIDs []string,
) (SelfCorrectionCandidateRecord, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	records, err := t.mergedSelfCorrectionCandidatesLocked()
	if err != nil {
		return SelfCorrectionCandidateRecord{}, false, err
	}
	excluded := make(map[string]bool, len(excludedIDs))
	for _, id := range excludedIDs {
		if id = strings.TrimSpace(id); id != "" {
			excluded[id] = true
		}
	}
	impactPolicies := selfCorrectionImpactPolicies(records)
	var selected SelfCorrectionCandidateRecord
	found := false
	for _, record := range records {
		if excluded[record.ID] || !SelfCorrectionDispatchEligible(record) ||
			!selfCorrectionImpactPolicyAllows(record, impactPolicies) {
			continue
		}
		if !found || dispatchCandidateBefore(record, selected, impactPolicies) {
			selected = record
			found = true
		}
	}
	return selected, found, nil
}

// maxSelfCorrectionDispatchFailures bounds how many times an unattended coding
// session may fail to land the same candidate before it is withheld from
// dispatch and left for operator review. rsilifecycle.CanDispatch treats a
// "failed" delivery phase as re-eligible (transient process failures deserve a
// retry), but a candidate that keeps failing is almost always impossible for an
// unattended session — doctrine-conflicting or too large — and every retry burns
// a coding session. Three attempts is enough signal to stop; the record stays in
// the ledger (accepted) and reappears in the accepted list for a human decision.
const maxSelfCorrectionDispatchFailures = 3

// selfCorrectionDispatchFailureCapReached reports whether a candidate has failed
// to land often enough that it is withheld from dispatch and left for operator
// review. It is the single source of the cap so eligibility and the RSI status
// tally agree on which candidates are withheld.
func selfCorrectionDispatchFailureCapReached(record SelfCorrectionCandidateRecord) bool {
	return record.DispatchFailures >= maxSelfCorrectionDispatchFailures
}

// SelfCorrectionDispatchEligible applies review, delivery, source, and surface
// policy before any unattended coding session receives candidate prose.
func SelfCorrectionDispatchEligible(record SelfCorrectionCandidateRecord) bool {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Scope) != "code" {
		return false
	}
	if selfCorrectionDispatchFailureCapReached(record) {
		return false
	}
	if !rsilifecycle.CanDispatch(
		rsilifecycle.ReviewState(record.Status),
		rsilifecycle.DeliveryPhase(record.DispatchPhase),
	) || !SourceAutoDispatches(record.Source) {
		return false
	}
	values := []string{
		record.Title,
		record.SkillName,
		record.Candidate,
		record.ProposedChange,
		record.Evidence,
		record.Risk,
	}
	values = append(values, record.TargetFiles...)
	return len(surfaces.ForbiddenSurfaceMentions(values...)) == 0
}

func dispatchCandidateBefore(
	left, right SelfCorrectionCandidateRecord,
	impactPolicies map[string]*selfCorrectionImpactPolicy,
) bool {
	leftAccepted := normalizeSelfCorrectionStatus(left.Status) == SelfCorrectionStatusAccepted
	rightAccepted := normalizeSelfCorrectionStatus(right.Status) == SelfCorrectionStatusAccepted
	if leftAccepted != rightAccepted {
		return leftAccepted
	}
	leftImpact := selfCorrectionImpactPriority(left, impactPolicies)
	rightImpact := selfCorrectionImpactPriority(right, impactPolicies)
	if leftImpact != rightImpact {
		return leftImpact > rightImpact
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt > right.CreatedAt
	}
	return left.ID < right.ID
}

// selfCorrectionImpactPolicy is a bounded, source-level view of usefulness
// history. Only the latest two terminal outcomes are needed for the consecutive
// failure gate, so building every policy stays O(n) without per-source sorting.
type selfCorrectionImpactPolicy struct {
	latest             selfCorrectionImpactOutcome
	secondLatest       selfCorrectionImpactOutcome
	negativeStrategies map[string]struct{}
}

type selfCorrectionImpactOutcome struct {
	status    string
	checkedAt int64
	id        string
}

func selfCorrectionImpactPolicies(
	records map[string]SelfCorrectionCandidateRecord,
) map[string]*selfCorrectionImpactPolicy {
	policies := make(map[string]*selfCorrectionImpactPolicy)
	for _, record := range records {
		result := record.ImpactResult
		source := strings.TrimSpace(record.Source)
		if source == "" || result == nil || result.CheckedAt <= 0 ||
			!selfCorrectionTerminalImpact(result.Status) {
			continue
		}
		policy := policies[source]
		if policy == nil {
			policy = &selfCorrectionImpactPolicy{}
			policies[source] = policy
		}
		outcome := selfCorrectionImpactOutcome{
			status: strings.TrimSpace(result.Status), checkedAt: result.CheckedAt, id: record.ID,
		}
		policy.record(outcome)
		if selfCorrectionNegativeImpact(outcome.status) {
			if strategy := normalizeSelfCorrectionStrategy(record.ProposedChange); strategy != "" {
				if policy.negativeStrategies == nil {
					policy.negativeStrategies = make(map[string]struct{})
				}
				policy.negativeStrategies[strategy] = struct{}{}
			}
		}
	}
	return policies
}

func (p *selfCorrectionImpactPolicy) record(outcome selfCorrectionImpactOutcome) {
	if selfCorrectionImpactOutcomeBefore(outcome, p.latest) {
		p.secondLatest = p.latest
		p.latest = outcome
	} else if selfCorrectionImpactOutcomeBefore(outcome, p.secondLatest) {
		p.secondLatest = outcome
	}
}

func selfCorrectionImpactOutcomeBefore(left, right selfCorrectionImpactOutcome) bool {
	if left.checkedAt != right.checkedAt {
		return left.checkedAt > right.checkedAt
	}
	return right.id == "" || left.id < right.id
}

func selfCorrectionTerminalImpact(status string) bool {
	switch strings.TrimSpace(status) {
	case selfCorrectionImpactVerified, selfCorrectionImpactNoEffect, selfCorrectionImpactRegressed:
		return true
	default:
		return false
	}
}

func selfCorrectionNegativeImpact(status string) bool {
	return status == selfCorrectionImpactNoEffect || status == selfCorrectionImpactRegressed
}

func selfCorrectionImpactPolicyAllows(
	record SelfCorrectionCandidateRecord,
	policies map[string]*selfCorrectionImpactPolicy,
) bool {
	policy := policies[strings.TrimSpace(record.Source)]
	if policy == nil || !selfCorrectionNegativeImpact(policy.latest.status) ||
		!selfCorrectionNegativeImpact(policy.secondLatest.status) {
		return true
	}
	strategy := normalizeSelfCorrectionStrategy(record.ProposedChange)
	if strategy == "" {
		return false
	}
	_, repeated := policy.negativeStrategies[strategy]
	return !repeated
}

func selfCorrectionImpactPriority(
	record SelfCorrectionCandidateRecord,
	policies map[string]*selfCorrectionImpactPolicy,
) int {
	policy := policies[strings.TrimSpace(record.Source)]
	if policy == nil {
		return 0
	}
	switch policy.latest.status {
	case selfCorrectionImpactRegressed:
		return 2
	case selfCorrectionImpactNoEffect:
		return 1
	default:
		return 0
	}
}

func normalizeSelfCorrectionStrategy(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
