// search_dedup.go — Post-search content deduplication using Jaccard text similarity.
// Removes near-duplicate facts from search results before they reach the LLM context,
// improving signal-to-noise ratio and reducing token waste.
package memory

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// jaccardTokenRe matches Unicode letters, numbers, and underscores.
// Unlike the Rust MMR tokenizer ([a-z0-9_]+), this handles Korean/CJK.
var jaccardTokenRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// Korean postposition/ending suffixes that frequently encode grammar rather
// than semantic intent in short fact snippets.
var koreanSemanticSuffixes = [...]string{
	"으로", "에서", "에게", "한테", "처럼", "까지", "부터",
	"으로서", "으로써", "보다", "마다", "조차", "라도",
	"을", "를", "이", "가", "은", "는", "에", "와", "과", "도", "만", "의", "로",
}

// Cross-language filler tokens that should not influence semantic dedup.
var dedupStopTokens = map[string]struct{}{
	"via": {}, "with": {}, "using": {}, "through": {},
	"위한": {}, "통한": {}, "필요": {}, "적용": {}, "방법": {}, "방안": {},
}

// jaccardTokenize returns the set of lowercased tokens in s.
func jaccardTokenize(s string) map[string]struct{} {
	lowered := strings.ToLower(s)
	matches := jaccardTokenRe.FindAllString(lowered, -1)
	set := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		m = normalizeDedupToken(m)
		if m == "" {
			continue
		}
		set[m] = struct{}{}
	}
	return set
}

func normalizeDedupToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if _, stop := dedupStopTokens[token]; stop {
		return ""
	}

	if containsHangul(token) {
		token = stripKoreanSuffix(token)
		if token == "" {
			return ""
		}
		if _, stop := dedupStopTokens[token]; stop {
			return ""
		}
	}
	return token
}

func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

func stripKoreanSuffix(token string) string {
	for _, suffix := range koreanSemanticSuffixes {
		if strings.HasSuffix(token, suffix) {
			base := strings.TrimSuffix(token, suffix)
			// Keep at least 2 runes to avoid over-trimming short words.
			if utf8.RuneCountInString(base) >= 2 {
				return base
			}
		}
	}
	return token
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

// JaccardTextSimilarity computes Jaccard similarity between two text strings.
// Tokenizes both strings and compares their token sets. Exported for use in
// InsertFact dedup and AutoExtractFacts pre-check.
func JaccardTextSimilarity(a, b string) float64 {
	return jaccardSimilarity(jaccardTokenize(a), jaccardTokenize(b))
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
