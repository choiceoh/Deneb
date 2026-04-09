package model

import "strings"

// SplitProviderModel splits a "provider/model" reference into [provider, model].
// If no provider is given, provider is empty and model is returned as-is.
func SplitProviderModel(ref string) [2]string {
	if idx := strings.IndexByte(ref, '/'); idx >= 0 {
		return [2]string{ref[:idx], ref[idx+1:]}
	}
	return [2]string{"", ref}
}

// ModelSelection holds the resolved model for a reply.
type ModelSelection struct {
	Provider    string
	Model       string
	IsOverride  bool
	IsFallback  bool
	AuthProfile string
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
		score := ScoreFuzzyMatch(lowered, candidates[i])
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

// ScoreFuzzyMatch computes a similarity score (0-100) between query and candidate.
func ScoreFuzzyMatch(query string, candidate ModelCandidate) int {
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

	return 0
}

// FormatProviderModelRef formats a provider/model reference string.
func FormatProviderModelRef(provider, model string) string {
	if provider == "" {
		return model
	}
	return provider + "/" + model
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
