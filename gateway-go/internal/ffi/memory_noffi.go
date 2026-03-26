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

// MemoryMergeHybridResults is a pure-Go fallback for hybrid search merge.
// Merges vector and keyword results with weighted scoring, sorted by score descending.
func MemoryMergeHybridResults(paramsJSON string) (json.RawMessage, error) {
	if len(paramsJSON) == 0 {
		return json.RawMessage("[]"), nil
	}

	var params struct {
		Vector       []hybridResult `json:"vector"`
		Keyword      []hybridResult `json:"keyword"`
		VectorWeight float64        `json:"vectorWeight"`
		TextWeight   float64        `json:"textWeight"`
	}
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return json.RawMessage("[]"), nil
	}

	type entry struct {
		Path        string  `json:"path"`
		StartLine   uint32  `json:"startLine"`
		EndLine     uint32  `json:"endLine"`
		Score       float64 `json:"score"`
		Snippet     string  `json:"snippet"`
		Source      string  `json:"source"`
		vectorScore float64
		textScore   float64
	}

	byID := make(map[string]*entry)

	for _, r := range params.Vector {
		byID[r.ID] = &entry{
			Path: r.Path, StartLine: r.StartLine, EndLine: r.EndLine,
			Source: r.Source, Snippet: r.Snippet, vectorScore: r.Score,
		}
	}
	for _, r := range params.Keyword {
		if e, ok := byID[r.ID]; ok {
			e.textScore = r.Score
			if r.Snippet != "" {
				e.Snippet = r.Snippet
			}
		} else {
			byID[r.ID] = &entry{
				Path: r.Path, StartLine: r.StartLine, EndLine: r.EndLine,
				Source: r.Source, Snippet: r.Snippet, textScore: r.Score,
			}
		}
	}

	merged := make([]entry, 0, len(byID))
	for _, e := range byID {
		e.Score = params.VectorWeight*e.vectorScore + params.TextWeight*e.textScore
		merged = append(merged, *e)
	}

	// Sort by score descending, path+startLine as tiebreaker.
	for i := 1; i < len(merged); i++ {
		for j := i; j > 0; j-- {
			if merged[j].Score > merged[j-1].Score ||
				(merged[j].Score == merged[j-1].Score && merged[j].Path < merged[j-1].Path) ||
				(merged[j].Score == merged[j-1].Score && merged[j].Path == merged[j-1].Path && merged[j].StartLine < merged[j-1].StartLine) {
				merged[j], merged[j-1] = merged[j-1], merged[j]
			}
		}
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return json.RawMessage("[]"), nil
	}
	return json.RawMessage(data), nil
}

type hybridResult struct {
	ID        string  `json:"id"`
	Path      string  `json:"path"`
	StartLine uint32  `json:"startLine"`
	EndLine   uint32  `json:"endLine"`
	Source    string  `json:"source"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
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
