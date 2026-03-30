// search_dedup.go — Post-search content deduplication using Jaccard text similarity.
// Removes near-duplicate facts from search results before they reach the LLM context,
// improving signal-to-noise ratio and reducing token waste.
package memory

import (
	"regexp"
	"strings"
)

// dedupJaccardThreshold is the Jaccard similarity above which two facts are
// considered near-duplicates. At 0.60, two facts share 60%+ of their unique
// tokens — clearly the same concept with minor wording differences.
const dedupJaccardThreshold = 0.60

// jaccardTokenRe matches Unicode letters, numbers, and underscores.
// Unlike the Rust MMR tokenizer ([a-z0-9_]+), this handles Korean/CJK.
var jaccardTokenRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// jaccardTokenize returns the set of lowercased tokens in s.
func jaccardTokenize(s string) map[string]struct{} {
	lowered := strings.ToLower(s)
	matches := jaccardTokenRe.FindAllString(lowered, -1)
	set := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		set[m] = struct{}{}
	}
	return set
}

// jaccardSimilarity computes the Jaccard index between two token sets.
func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0 // two empty facts are not "similar" — nothing to compare
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Iterate over the smaller set for efficiency.
	smaller, larger := a, b
	if len(a) > len(b) {
		smaller, larger = b, a
	}

	intersection := 0
	for tok := range smaller {
		if _, ok := larger[tok]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// dedupResults removes near-duplicate results using greedy Jaccard filtering.
// Results must arrive sorted by score descending (from mergeAndRank).
// For each candidate, if its content has Jaccard similarity > threshold with
// any already-accepted result, it is dropped in favor of the higher-scored one.
func dedupResults(results []SearchResult, threshold float64) []SearchResult {
	if len(results) <= 1 {
		return results
	}

	type accepted struct {
		result SearchResult
		tokens map[string]struct{}
	}

	kept := make([]accepted, 0, len(results))

	for _, r := range results {
		tokens := jaccardTokenize(r.Fact.Content)
		isDup := false

		for _, a := range kept {
			if jaccardSimilarity(tokens, a.tokens) > threshold {
				isDup = true
				break
			}
		}

		if !isDup {
			kept = append(kept, accepted{result: r, tokens: tokens})
		}
	}

	out := make([]SearchResult, len(kept))
	for i, a := range kept {
		out[i] = a.result
	}
	return out
}
