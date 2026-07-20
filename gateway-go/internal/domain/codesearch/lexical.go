package codesearch

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type rankedIndex struct {
	idx   int
	score float64
}

// lexicalRanks is an index-local BM25 arm over the exact contextual text that
// was embedded. CodeGraph FTS only knows parsed symbol fields; this arm also
// covers file paths, source excerpts, scripts, configuration, and documents.
func lexicalRanks(entries []Entry, query string, limit int) []rankedIndex {
	terms := uniqueSearchTerms(query)
	if len(terms) == 0 || len(entries) == 0 || limit <= 0 {
		return nil
	}
	termSet := make(map[string]bool, len(terms))
	for _, term := range terms {
		termSet[term] = true
	}
	type docStats struct {
		tf     map[string]int
		length int
	}
	stats := make([]docStats, len(entries))
	df := make(map[string]int, len(terms))
	totalLength := 0
	for i, entry := range entries {
		tokens := lexicalTokens(entry.Lexical)
		st := docStats{tf: make(map[string]int), length: len(tokens)}
		seen := make(map[string]bool)
		for _, token := range tokens {
			if !termSet[token] {
				continue
			}
			st.tf[token]++
			if !seen[token] {
				df[token]++
				seen[token] = true
			}
		}
		stats[i] = st
		totalLength += max(1, st.length)
	}
	avgLength := float64(totalLength) / float64(len(entries))
	if avgLength < 1 {
		avgLength = 1
	}
	const k1, b = 1.2, 0.75
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	out := make([]rankedIndex, 0, min(limit*2, len(entries)))
	for i, entry := range entries {
		st := stats[i]
		fieldSet := make(map[string]bool)
		for _, token := range lexicalTokens(entry.File + " " + entry.Qualified) {
			fieldSet[token] = true
		}
		var score float64
		for _, term := range terms {
			tf := st.tf[term]
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(entries)-df[term])+0.5)/(float64(df[term])+0.5))
			denom := float64(tf) + k1*(1-b+b*float64(max(1, st.length))/avgLength)
			score += idf * (float64(tf) * (k1 + 1)) / denom
			if fieldSet[term] {
				score += idf * 0.8
			}
		}
		if lowerQuery != "" && strings.Contains(strings.ToLower(entry.Lexical), lowerQuery) {
			score += 2
		}
		if score > 0 {
			out = append(out, rankedIndex{idx: i, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		a, z := entries[out[i].idx], entries[out[j].idx]
		if a.File != z.File {
			return a.File < z.File
		}
		return a.StartLine < z.StartLine
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func uniqueSearchTerms(text string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, token := range lexicalTokens(text) {
		if utf8.RuneCountInString(token) < 2 || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

// lexicalTokens keeps whole identifiers and also emits camel-case components:
// buildTailAdditions becomes buildtailadditions, build, tail, additions. This
// makes a natural-language phrase meet the symbol without destructive query
// splitting or multiple embedding calls.
func lexicalTokens(text string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		raw := string(word)
		lower := strings.ToLower(raw)
		out = append(out, lower)
		parts := splitCamel(raw)
		if len(parts) > 1 {
			for _, part := range parts {
				part = strings.ToLower(part)
				if part != "" && part != lower {
					out = append(out, part)
				}
			}
		}
		word = word[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func splitCamel(raw string) []string {
	runes := []rune(raw)
	if len(runes) < 2 {
		return []string{raw}
	}
	starts := []int{0}
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		boundary := (unicode.IsLower(prev) && unicode.IsUpper(cur)) ||
			(unicode.IsLetter(prev) && unicode.IsDigit(cur)) ||
			(unicode.IsDigit(prev) && unicode.IsLetter(cur)) ||
			(unicode.IsUpper(prev) && unicode.IsUpper(cur) && nextLower)
		if boundary {
			starts = append(starts, i)
		}
	}
	if len(starts) == 1 {
		return []string{raw}
	}
	parts := make([]string, 0, len(starts))
	for i, start := range starts {
		end := len(runes)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}
