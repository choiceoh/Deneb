// fact_resolve.go — conflict resolution policy. Decides what one assertion or
// tombstone does to the claims already held for an identity: who wins, what is
// merely recorded, and which older claims change status in the same atomic
// decision. Durability lives in fact_store.go.

package wiki

import (
	"fmt"
	"strings"
	"time"
)

func (s *Store) resolveFactMutationLocked(input FactInput, now time.Time) FactMutation {
	identity := factIdentity(input.Subject, input.Key)
	revision := s.factState.Revision + 1
	basisAtMs := int64(0)
	if !input.BasisAt.IsZero() {
		basisAtMs = input.BasisAt.UTC().UnixMilli()
	}
	claim := FactClaim{
		Subject: input.Subject, Key: input.Key, Value: input.Value,
		Kind: input.Kind, Authority: input.Authority,
		Sources: input.Sources, Actor: input.Actor, Reason: input.Reason,
		RecordedAtMs: now.UnixMilli(), BasisAtMs: basisAtMs, Revision: revision,
	}
	claim.ID = newFactOperationID(revision, identity, input.Value, claim.RecordedAtMs)

	claims := s.factState.Facts[identity]
	active := make([]FactClaim, 0, len(claims))
	for i := range claims {
		switch claims[i].Status {
		case FactStatusCurrent, FactStatusConflicted:
			active = append(active, claims[i])
		case FactStatusSuperseded, FactStatusTombstoned:
		}
	}
	tombstoneBarrier := strongestFactTombstone(claims)
	updates := map[string]FactStatus{}
	resolution := "create"
	op := "assert"
	incomingRank := factAuthorityRank(input.Kind, input.Authority)

	if len(active) == 0 && tombstoneBarrier != nil {
		if incomingRank < tombstoneBarrier.rank ||
			(incomingRank == tombstoneBarrier.rank &&
				(!factUsesLatestPolicy(input.Kind, input.Authority) || factBasisTime(claim) <= tombstoneBarrier.atMs)) {
			claim.Status = FactStatusSuperseded
			resolution = "blocked_by_tombstone"
		}
	}
	if len(active) > 0 {
		maxRank := 0
		maxBasis := int64(0)
		sameValue := false
		for _, old := range active {
			rank := factAuthorityRank(old.Kind, old.Authority)
			if rank > maxRank {
				maxRank = rank
				maxBasis = factBasisTime(old)
			} else if rank == maxRank && factBasisTime(old) > maxBasis {
				maxBasis = factBasisTime(old)
			}
			if sameFactValue(old.Value, input.Value) {
				sameValue = true
			}
		}

		switch {
		case incomingRank > maxRank:
			claim.Status = FactStatusCurrent
			resolution = "higher_authority"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case incomingRank < maxRank:
			claim.Status = FactStatusSuperseded
			resolution = "ignored_lower_authority"
		case sameValue:
			claim.Status = FactStatusCurrent
			resolution = "reaffirmed"
			op = "reaffirm"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case factUsesLatestPolicy(input.Kind, input.Authority) && factBasisTime(claim) >= maxBasis:
			claim.Status = FactStatusCurrent
			resolution = "latest_authoritative"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case factUsesLatestPolicy(input.Kind, input.Authority):
			claim.Status = FactStatusSuperseded
			resolution = "ignored_older_basis"
		default:
			claim.Status = FactStatusConflicted
			resolution = "unresolved_conflict"
			markActiveFactStatuses(active, updates, FactStatusConflicted)
		}
	} else if claim.Status == "" {
		claim.Status = FactStatusCurrent
	}
	if len(updates) == 0 {
		updates = nil
	}
	return FactMutation{
		SchemaVersion: factSchemaVersion, Revision: revision,
		OperationID: claim.ID, AtMs: claim.RecordedAtMs, Op: op,
		Identity: identity, Claim: &claim, StatusUpdates: updates,
		Resolution: resolution, Reason: input.Reason,
	}
}

func factKindForIdentity(incoming FactKind, claims []FactClaim) (FactKind, error) {
	established := FactKind("")
	for _, claim := range claims {
		if claim.Kind == FactKindGeneric || claim.Kind == "" {
			continue
		}
		if established == "" {
			established = claim.Kind
			continue
		}
		if claim.Kind != established {
			return "", fmt.Errorf("fact identity history mixes kinds %q and %q", established, claim.Kind)
		}
	}
	if established == "" {
		if incoming != FactKindGeneric {
			for _, claim := range claims {
				if claim.Kind == FactKindGeneric && strings.TrimSpace(claim.Value) != "" && claim.Authority != FactAuthorityLegacyImport {
					return "", fmt.Errorf("only legacy generic facts may be promoted to kind %q", incoming)
				}
			}
		}
		return incoming, nil
	}
	if incoming == FactKindGeneric {
		return established, nil
	}
	if incoming != established {
		return "", fmt.Errorf("fact identity kind is %q, not %q", established, incoming)
	}
	return established, nil
}

func (s *Store) resolveFactTombstoneLocked(input FactTombstoneInput, now time.Time) FactMutation {
	identity := factIdentity(input.Subject, input.Key)
	revision := s.factState.Revision + 1
	claims := s.factState.Facts[identity]
	kind := FactKindGeneric
	updates := map[string]FactStatus{}
	activeCount := 0
	for i := len(claims) - 1; i >= 0; i-- {
		if claims[i].Kind != "" {
			kind = claims[i].Kind
			break
		}
	}
	incomingRank := factAuthorityRank(kind, input.Authority)
	requiredRank := 0
	for _, claim := range claims {
		if claim.Status != FactStatusCurrent && claim.Status != FactStatusConflicted && claim.Status != FactStatusTombstoned {
			continue
		}
		rank := factAuthorityRank(claim.Kind, claim.Authority)
		if rank > requiredRank {
			requiredRank = rank
		}
		if claim.Status == FactStatusCurrent || claim.Status == FactStatusConflicted {
			activeCount++
		}
	}
	claim := FactClaim{
		Subject: input.Subject, Key: input.Key, Kind: kind,
		Authority: input.Authority, Status: FactStatusTombstoned,
		Sources: input.Sources, Actor: input.Actor, Reason: input.Reason,
		RecordedAtMs: now.UnixMilli(), Revision: revision,
	}
	claim.ID = newFactOperationID(revision, identity, "<tombstone>", claim.RecordedAtMs)
	resolution := "tombstoned"
	if incomingRank < requiredRank && input.Authority != FactAuthorityDirectUser {
		// Lower-authority deletes are retained for audit but cannot retire a
		// stronger live claim. In particular, an agent_confirmed tool call must not
		// erase a direct-user fact merely because it is an explicit lifecycle action.
		// DirectUser is the sole exception: it is minted only by the trusted privacy
		// forget path, where an explicit user deletion must override document/runtime
		// authority regardless of rank.
		claim.Status = FactStatusSuperseded
		resolution = "ignored_lower_authority"
	} else {
		for _, old := range claims {
			if old.Status == FactStatusCurrent || old.Status == FactStatusConflicted {
				updates[old.ID] = FactStatusTombstoned
			}
		}
		if activeCount == 0 {
			resolution = "already_absent"
		}
	}
	if len(updates) == 0 {
		updates = nil
	}
	return FactMutation{
		SchemaVersion: factSchemaVersion, Revision: revision,
		OperationID: claim.ID, AtMs: claim.RecordedAtMs, Op: "tombstone",
		Identity: identity, Claim: &claim, StatusUpdates: updates,
		Resolution: resolution, Reason: input.Reason,
	}
}

func sameFactValue(left, right string) bool {
	left = strings.ToLower(strings.Join(strings.Fields(left), " "))
	right = strings.ToLower(strings.Join(strings.Fields(right), " "))
	return left == right
}

func markActiveFactStatuses(active []FactClaim, updates map[string]FactStatus, status FactStatus) {
	for _, old := range active {
		updates[old.ID] = status
	}
}

type factTombstoneBarrier struct {
	claim FactClaim
	rank  int
	atMs  int64
}

func strongestFactTombstone(claims []FactClaim) *factTombstoneBarrier {
	var barrier *factTombstoneBarrier
	for i := range claims {
		if claims[i].Status != FactStatusTombstoned {
			continue
		}
		rank := factAuthorityRank(claims[i].Kind, claims[i].Authority)
		atMs := factBasisTime(claims[i])
		if barrier == nil {
			barrier = &factTombstoneBarrier{claim: claims[i], rank: rank, atMs: atMs}
			continue
		}
		if atMs > barrier.atMs {
			barrier.atMs = atMs
		}
		if rank > barrier.rank ||
			(rank == barrier.rank && (atMs > factBasisTime(barrier.claim) ||
				(atMs == factBasisTime(barrier.claim) && claims[i].Revision > barrier.claim.Revision))) {
			barrier.claim = claims[i]
			barrier.rank = rank
		}
	}
	return barrier
}
