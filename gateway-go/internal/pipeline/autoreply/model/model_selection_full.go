// model_selection_full.go — Full model selection with stored overrides and fuzzy matching.
package model

import (
	"strings"
)

// ModelSelectionState holds the full resolved model state.
type ModelSelectionState struct {
	Provider     string
	Model        string
	AuthProfile  string
	IsOverride   bool
	IsFallback   bool
	IsDefault    bool
	SelectedBy   string // "directive", "session", "config", "default"
	FuzzyMatched bool
}

// ModelSelectionConfig configures model resolution.
type ModelSelectionConfig struct {
	DefaultProvider   string
	DefaultModel      string
	SessionModel      string // from session store
	SessionProvider   string
	ConfigModel       string // from config file
	ConfigProvider    string
	DirectiveModel    string // from /model directive
	DirectiveProvider string
	DirectiveProfile  string
	Candidates        []ModelCandidate
	FallbackModels    []string
}

// ResolveModelSelection performs the full model resolution pipeline.
// Priority: directive > session > config > default.
func ResolveModelSelection(cfg ModelSelectionConfig) ModelSelectionState {
	// 1. Directive override (highest priority).
	if cfg.DirectiveModel != "" {
		resolved := resolveFromCandidates(cfg.DirectiveModel, cfg.DirectiveProvider, cfg.Candidates)
		if resolved != nil {
			return ModelSelectionState{
				Provider:     resolved.Provider,
				Model:        resolved.Model,
				AuthProfile:  cfg.DirectiveProfile,
				IsOverride:   true,
				SelectedBy:   "directive",
				FuzzyMatched: resolved.FuzzyMatched,
			}
		}
		// Accept raw directive value even without candidate match.
		parts := SplitProviderModel(cfg.DirectiveModel)
		provider, model := parts[0], parts[1]
		if cfg.DirectiveProvider != "" {
			provider = cfg.DirectiveProvider
		}
		return ModelSelectionState{
			Provider:    provider,
			Model:       model,
			AuthProfile: cfg.DirectiveProfile,
			IsOverride:  true,
			SelectedBy:  "directive",
		}
	}

	// 2. Session override (stored from previous /model command).
	if cfg.SessionModel != "" {
		return ModelSelectionState{
			Provider:   cfg.SessionProvider,
			Model:      cfg.SessionModel,
			IsOverride: true,
			SelectedBy: "session",
		}
	}

	// 3. Config default.
	if cfg.ConfigModel != "" {
		return ModelSelectionState{
			Provider:   cfg.ConfigProvider,
			Model:      cfg.ConfigModel,
			SelectedBy: "config",
		}
	}

	// 4. System default.
	return ModelSelectionState{
		Provider:   cfg.DefaultProvider,
		Model:      cfg.DefaultModel,
		IsDefault:  true,
		SelectedBy: "default",
	}
}

type resolvedCandidate struct {
	Provider     string
	Model        string
	FuzzyMatched bool
}

func resolveFromCandidates(rawModel, rawProvider string, candidates []ModelCandidate) *resolvedCandidate {
	if len(candidates) == 0 {
		return nil
	}

	// Try exact match.
	resolved := ResolveModelFromDirective(rawModel, candidates)
	if resolved != nil {
		return &resolvedCandidate{
			Provider: resolved.Provider,
			Model:    resolved.Model,
		}
	}

	// Try with explicit provider prefix.
	if rawProvider != "" {
		fullRef := rawProvider + "/" + rawModel
		resolved = ResolveModelFromDirective(fullRef, candidates)
		if resolved != nil {
			return &resolvedCandidate{
				Provider: resolved.Provider,
				Model:    resolved.Model,
			}
		}
	}

	// Fuzzy match.
	lowered := strings.ToLower(rawModel)
	var bestCandidate *ModelCandidate
	bestScore := 0
	for i := range candidates {
		score := ScoreFuzzyMatch(lowered, candidates[i])
		if score > bestScore {
			bestScore = score
			bestCandidate = &candidates[i]
		}
	}
	if bestCandidate != nil && bestScore >= 40 {
		return &resolvedCandidate{
			Provider:     bestCandidate.Provider,
			Model:        bestCandidate.Model,
			FuzzyMatched: true,
		}
	}
	return nil
}
