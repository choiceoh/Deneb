// query_expansion.go — LLM query-expansion BACKFILL for vocabulary-gap queries.
//
// The 2026-07 retrieval cycle measured the only positive recall lever left:
// LLM expansion bridges query vocabulary to wiki vocabulary ("독소조항" →
// "위약벌·지체상금"). But the naive shape — RRF-merging expansion hits with the
// original ranking — cost hit@1 badly (−18 cases on the 07-21 sweep): expansion
// noise outranked exact matches. Backfill is the safe shape that survived:
// original results keep their order untouched, and expansion hits only FILL
// REMAINING SLOTS when the original query under-fills the limit — which is
// precisely the vocabulary-gap case expansion exists for. A query that already
// fills its limit is byte-identical to today (structural p@1 no-regression).
//
// Dormant by default: no expander + env off ⇒ zero behavior change. The
// expander is injected by the caller (gateway boot or recall-bench) so this
// package stays LLM-free; the model/role choice lives with the injector.
package wiki

import (
	"context"
	"os"
	"strings"
)

// QueryExpander turns a user intent into a small set of alternate Korean
// search phrasings (wiki-vocabulary bridges). Implementations are injected via
// SetQueryExpander; nil disables expansion entirely.
type QueryExpander func(ctx context.Context, intent string) []string

// expansionMode reads the rollout gate. Only "backfill" activates; every other
// value (including unset) is off, so production behavior cannot change without
// an explicit env opt-in.
func expansionMode() string {
	return strings.TrimSpace(os.Getenv("DENEB_WIKI_QUERY_EXPANSION"))
}

// expansionMaxTerms bounds the LLM's contribution per query: two bridges cover
// the measured gap cases; more terms only added noise in the 07-21 sweep.
const expansionMaxTerms = 2

// SetQueryExpander injects the expansion callback (nil to disable). Callers
// own model/role selection and timeouts; the store only ever calls it when the
// backfill gate is on AND a query under-fills its limit.
func (s *Store) SetQueryExpander(fn QueryExpander) {
	if s == nil {
		return
	}
	s.queryExpander = fn
}

// backfillWithExpansion fills the tail of an under-filled result list with
// hits found only by the expanded phrasings. Original results and their order
// are preserved verbatim; expansion hits score strictly below the weakest
// original hit so no downstream consumer can rank them above the primary
// evidence. Returns results unchanged when the gate is off, no expander is
// set, or the primary query already filled the limit.
func (s *Store) backfillWithExpansion(ctx context.Context, intent string, results []SearchResult, limit int) []SearchResult {
	if s == nil || s.queryExpander == nil || expansionMode() != "backfill" {
		return results
	}
	if limit <= 0 || len(results) >= limit || strings.TrimSpace(intent) == "" {
		return results
	}
	terms := s.queryExpander(ctx, intent)
	if len(terms) > expansionMaxTerms {
		terms = terms[:expansionMaxTerms]
	}
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		seen[r.Path] = struct{}{}
	}
	// Score floor: strictly below the weakest original hit (or a nominal floor
	// when the original found nothing) so ordering stays original-first.
	floor := 0.10
	if n := len(results); n > 0 && results[n-1].Score < floor {
		floor = results[n-1].Score
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || len(results) >= limit {
			break
		}
		// skipMetadata marks this as an internal sub-search: the outer caller
		// attaches metadata after backfill, and the flag keeps this lookup out
		// of the backfill hook itself (no recursion).
		hits, err := s.SearchWithOptions(ctx, term, limit, QueryOptions{
			Mode:         SearchModeBM25,
			SkipRerank:   true,
			skipMetadata: true,
		})
		if err != nil {
			continue // expansion is best-effort; the primary results stand
		}
		for _, h := range hits.Results {
			if len(results) >= limit {
				break
			}
			if _, dup := seen[h.Path]; dup {
				continue
			}
			seen[h.Path] = struct{}{}
			floor *= 0.95 // keep appended hits in descending, sub-original order
			h.Score = floor
			results = append(results, h)
		}
	}
	return results
}
