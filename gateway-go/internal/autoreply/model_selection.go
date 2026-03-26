package autoreply

import (
	"strings"
	"unicode/utf8"
)

// ModelSelection holds the resolved model for a reply.
type ModelSelection struct {
	Provider     string
	Model        string
	IsOverride   bool
	IsFallback   bool
	AuthProfile  string
}

// ModelCandidate is a model available for selection.
type ModelCandidate struct {
	Provider string
	Model    string
	Label    string
	Aliases  []string
}

// ResolveModelFromDirective resolves a model from a /model directive value.
// Returns the best matching candidate or nil.
func ResolveModelFromDirective(raw string, candidates []ModelCandidate) *ModelCandidate {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	lowered := strings.ToLower(trimmed)

	// Exact match by provider/model.
	for i := range candidates {
		ref := FormatProviderModelRef(candidates[i].Provider, candidates[i].Model)
		if strings.ToLower(ref) == lowered {
			return &candidates[i]
		}
	}

	// Exact match by model ID only.
	for i := range candidates {
		if strings.ToLower(candidates[i].Model) == lowered {
			return &candidates[i]
		}
	}

	// Alias match.
	for i := range candidates {
		for _, alias := range candidates[i].Aliases {
			if strings.ToLower(alias) == lowered {
				return &candidates[i]
			}
		}
	}

	// Fuzzy match: find best candidate by edit distance.
	var best *ModelCandidate
	bestScore := -1
	for i := range candidates {
		score := scoreFuzzyMatch(lowered, candidates[i])
		if score > bestScore {
			bestScore = score
			best = &candidates[i]
		}
	}
	if bestScore >= 50 {
		return best
	}
	return nil
}

// scoreFuzzyMatch computes a similarity score (0-100) between query and candidate.
func scoreFuzzyMatch(query string, candidate ModelCandidate) int {
	modelLow := strings.ToLower(candidate.Model)
	labelLow := strings.ToLower(candidate.Label)

	// Prefix match scores high.
	if strings.HasPrefix(modelLow, query) {
		return 90
	}
	if strings.HasPrefix(labelLow, query) {
		return 85
	}

	// Contains match.
	if strings.Contains(modelLow, query) {
		return 70
	}
	if strings.Contains(labelLow, query) {
		return 65
	}

	// Bounded Levenshtein distance.
	dist := boundedLevenshtein(query, modelLow, 3)
	if dist >= 0 && dist <= 2 {
		return 60 - dist*10
	}
	return 0
}

// boundedLevenshtein computes edit distance up to maxDist.
// Returns -1 if distance exceeds maxDist.
func boundedLevenshtein(a, b string, maxDist int) int {
	la := utf8.RuneCountInString(a)
	lb := utf8.RuneCountInString(b)
	if abs(la-lb) > maxDist {
		return -1
	}

	ra := []rune(a)
	rb := []rune(b)

	// Use two rows for space efficiency.
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		minInRow := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,     // deletion
				curr[j-1]+1,   // insertion
				prev[j-1]+cost, // substitution
			)
			if curr[j] < minInRow {
				minInRow = curr[j]
			}
		}
		if minInRow > maxDist {
			return -1
		}
		prev, curr = curr, prev
	}

	if prev[lb] > maxDist {
		return -1
	}
	return prev[lb]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// ResolveModelOverride checks session and config for model overrides.
func ResolveModelOverride(sessionModel, configModel, defaultModel string) string {
	if sessionModel != "" {
		return sessionModel
	}
	if configModel != "" {
		return configModel
	}
	return defaultModel
}
