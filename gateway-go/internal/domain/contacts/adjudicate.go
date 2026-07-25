package contacts

// The deterministic Dedup pass leaves AmbiguousPairs — two entries that share a
// personal identifier but carry incompatible names. Only a judgement that reads
// the names as language can tell "강상민" from "브라이트 강상민" (same person) apart
// from two colleagues on one line. Resolve delegates that judgement to an injected
// Adjudicator so the domain stays pure: the LLM-backed implementation (batched,
// cloud) lives in the runtime layer and never leaks into this package.

import "context"

// Verdict is an adjudicator's ruling on one AmbiguousPair.
type Verdict int

const (
	// VerdictUnsure keeps the pair apart — the safe default, since a wrong merge
	// fabricates a person and is harder to notice than a missed duplicate.
	VerdictUnsure Verdict = iota
	// VerdictSame folds the two entries together (same person, different label).
	VerdictSame
	// VerdictDifferent keeps them apart (different people sharing a number).
	VerdictDifferent
)

// Adjudicator rules on ambiguous pairs the deterministic pass could not resolve.
// It receives the whole contact slice (for context) and the pairs to judge, and
// returns exactly one Verdict per pair, in the same order. Implementations batch
// however they like — an LLM adjudicator groups pairs per request.
type Adjudicator interface {
	Adjudicate(ctx context.Context, contacts []Contact, pairs []AmbiguousPair) ([]Verdict, error)
}

// Resolve applies adj to res.Ambiguous, folding VerdictSame pairs into the merge
// groups and re-deriving the result; Different/Unsure pairs stay apart and remain
// in Ambiguous. It re-runs union-find seeded with the existing deterministic
// merges, so a SAME verdict can transitively join two already-merged families.
// The receiver is not mutated. A nil adjudicator or empty Ambiguous is a no-op.
func (res DedupResult) Resolve(ctx context.Context, contacts []Contact, adj Adjudicator) (DedupResult, error) {
	if adj == nil || len(res.Ambiguous) == 0 {
		return res, nil
	}
	verdicts, err := adj.Adjudicate(ctx, contacts, res.Ambiguous)
	if err != nil {
		return res, err
	}

	uf := newUnionFind(len(contacts))
	for _, g := range res.Merges { // re-seed the deterministic clusters
		for _, m := range g.Members[1:] {
			uf.union(g.Members[0], m)
		}
	}
	var remaining []AmbiguousPair
	for i, pair := range res.Ambiguous {
		v := VerdictUnsure
		if i < len(verdicts) {
			v = verdicts[i]
		}
		if v == VerdictSame {
			uf.union(pair.A, pair.B)
		} else {
			remaining = append(remaining, pair) // Different / Unsure stay apart
		}
	}

	merges, distinct := mergeGroupsFromClusters(contacts, uf)
	return DedupResult{
		Merges:    merges,
		Ambiguous: remaining,
		Total:     res.Total,
		Distinct:  distinct,
	}, nil
}
