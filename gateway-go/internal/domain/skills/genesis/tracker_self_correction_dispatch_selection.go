package genesis

import (
	"strings"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"
)

// SelectSelfCorrectionDispatchCandidate chooses one candidate in O(n) without
// copying or sorting the queue. Review-approved work wins, then newest work.
// Local marker/worktree residue remains an executor concern and is supplied as
// excluded IDs by the RPC client.
func SelectSelfCorrectionDispatchCandidate(
	records []SelfCorrectionCandidateRecord,
	excludedIDs []string,
) (SelfCorrectionCandidateRecord, bool) {
	excluded := make(map[string]bool, len(excludedIDs))
	for _, id := range excludedIDs {
		if id = strings.TrimSpace(id); id != "" {
			excluded[id] = true
		}
	}
	var selected SelfCorrectionCandidateRecord
	found := false
	for _, record := range records {
		if excluded[record.ID] || !SelfCorrectionDispatchEligible(record) {
			continue
		}
		if !found || dispatchCandidateBefore(record, selected) {
			selected = record
			found = true
		}
	}
	return selected, found
}

// SelfCorrectionDispatchEligible applies review, delivery, source, and surface
// policy before any unattended coding session receives candidate prose.
func SelfCorrectionDispatchEligible(record SelfCorrectionCandidateRecord) bool {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Scope) != "code" {
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

func dispatchCandidateBefore(left, right SelfCorrectionCandidateRecord) bool {
	leftAccepted := normalizeSelfCorrectionStatus(left.Status) == SelfCorrectionStatusAccepted
	rightAccepted := normalizeSelfCorrectionStatus(right.Status) == SelfCorrectionStatusAccepted
	if leftAccepted != rightAccepted {
		return leftAccepted
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt > right.CreatedAt
	}
	return left.ID < right.ID
}
