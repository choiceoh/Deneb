// bm25.go — BM25 ranking for deferred-tool search (fetch_tools query path).
//
// Inspired by Hermes Agent's Tool Search: rank the deferred-tool catalog by
// BM25 over each tool's name + description + parameter names, so a keyword
// query returns the most relevant tools first instead of an unordered set of
// substring hits. When no query token matches any tool (the "zero-IDF"
// degenerate case), the caller falls back to a literal substring match so we
// never regress recall versus the old substring-only search.
package tools

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 tuning constants (standard defaults).
const (
	bm25K1 = 1.5  // term-frequency saturation
	bm25B  = 0.75 // length normalization
)

// searchDoc is one tool in the BM25 corpus.
type searchDoc struct {
	name     string   // tool name (returned to caller)
	tokens   []string // tokenized name + description + parameter names
	fallback string   // lowercased "name description" for substring fallback
}

// tokenize lowercases and splits a string on any non-alphanumeric rune.
// No stemming — the corpus is tiny and exact-prefix matching is good enough.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// extractParamNames pulls the top-level property keys from a JSON Schema object
// (the tool's input parameters). Returns nil for a nil/empty schema or one
// without a "properties" object.
func extractParamNames(schema map[string]any) []string {
	if len(schema) == 0 {
		return nil
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	return names
}

// bm25Rank scores docs against query and returns matching doc names ordered by
// descending relevance. Only docs with a positive score are returned. Returns
// nil when no query token matches any doc, signaling the caller to apply a
// substring fallback.
func bm25Rank(query string, docs []searchDoc) []string {
	qTokens := tokenize(query)
	if len(qTokens) == 0 || len(docs) == 0 {
		return nil
	}

	n := float64(len(docs))
	df := make(map[string]int)              // document frequency per term
	tf := make([]map[string]int, len(docs)) // term frequency per doc
	totalLen := 0
	for i, d := range docs {
		tf[i] = make(map[string]int)
		seen := make(map[string]bool)
		for _, tok := range d.tokens {
			tf[i][tok]++
			if !seen[tok] {
				df[tok]++
				seen[tok] = true
			}
		}
		totalLen += len(d.tokens)
	}
	avgdl := float64(totalLen) / n
	if avgdl == 0 {
		avgdl = 1
	}

	type scored struct {
		name  string
		score float64
		idx   int // corpus position, for stable tie-breaking
	}
	var results []scored
	for i, d := range docs {
		var score float64
		dl := float64(len(d.tokens))
		for _, q := range qTokens {
			f := float64(tf[i][q])
			if f == 0 {
				continue
			}
			// Lucene-style IDF: always >= 0, avoids negative scores.
			idf := math.Log(1 + (n-float64(df[q])+0.5)/(float64(df[q])+0.5))
			score += idf * (f * (bm25K1 + 1)) / (f + bm25K1*(1-bm25B+bm25B*dl/avgdl))
		}
		if score > 0 {
			results = append(results, scored{name: d.name, score: score, idx: i})
		}
	}
	if len(results) == 0 {
		return nil
	}

	sort.SliceStable(results, func(a, b int) bool {
		if results[a].score != results[b].score {
			return results[a].score > results[b].score
		}
		return results[a].idx < results[b].idx // stable: preserve corpus order on ties
	})
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.name
	}
	return out
}
