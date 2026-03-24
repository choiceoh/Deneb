//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode"
)

// MemoryCosineSimilarity is a pure-Go fallback for cosine similarity.
func MemoryCosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	var dotProduct, normA, normB float64
	for i := 0; i < minLen; i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// MemoryBm25RankToScore is a pure-Go fallback for BM25 rank-to-score conversion.
// Uses exponential decay: score = 1 / (1 + rank).
func MemoryBm25RankToScore(rank float64) float64 {
	if rank < 0 {
		rank = 0
	}
	return 1.0 / (1.0 + rank)
}

// MemoryBuildFtsQuery is a pure-Go fallback that tokenizes text for FTS.
func MemoryBuildFtsQuery(raw string) (string, error) {
	if len(strings.TrimSpace(raw)) == 0 {
		return "", nil
	}

	words := strings.FieldsFunc(raw, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var tokens []string
	seen := make(map[string]bool)
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) < 2 || seen[lower] {
			continue
		}
		seen[lower] = true
		tokens = append(tokens, lower)
	}

	if len(tokens) == 0 {
		return "", nil
	}
	return strings.Join(tokens, " OR "), nil
}

// MemoryMergeHybridResults is a pure-Go fallback that returns an empty array.
// Full hybrid merge requires the Rust implementation.
func MemoryMergeHybridResults(_ string) (json.RawMessage, error) {
	return json.RawMessage("[]"), nil
}

// MemoryExtractKeywords is a pure-Go fallback for keyword extraction.
func MemoryExtractKeywords(query string) ([]string, error) {
	if len(strings.TrimSpace(query)) == 0 {
		return nil, nil
	}

	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	// Filter stop words and short tokens.
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true,
		"of": true, "in": true, "to": true, "for": true, "with": true,
		"on": true, "at": true, "by": true, "from": true, "as": true,
		"it": true, "this": true, "that": true, "and": true, "or": true,
		"not": true, "no": true, "but": true, "if": true, "so": true,
	}

	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) < 2 || stopWords[lower] || seen[lower] {
			continue
		}
		seen[lower] = true
		keywords = append(keywords, lower)
	}
	if keywords == nil {
		return []string{}, errors.New("ffi: no keywords extracted")
	}
	return keywords, nil
}
