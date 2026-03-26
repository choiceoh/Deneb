package autoreply

import (
	"regexp"
	"strings"
)

// ThinkingCatalogEntry represents a model entry in the thinking catalog.
type ThinkingCatalogEntry struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

// Model regexes for xhigh/adaptive thinking support detection.
var (
	anthropicClaude46ModelRe     = regexp.MustCompile("(?i)^claude-(?:opus|sonnet)-4(?:\\.|-)6(?:$|[-.])")
	amazonBedrockClaude46ModelRe = regexp.MustCompile("(?i)claude-(?:opus|sonnet)-4(?:\\.|-)6(?:$|[-.])")
)

// Model IDs that support xhigh thinking.
var (
	openaiXHighModelIDs = []string{
		"gpt-5.4", "gpt-5.4-pro", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.2",
	}
	openaiCodexXHighModelIDs = []string{
		"gpt-5.4", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2-codex", "gpt-5.1-codex",
	}
	githubCopilotXHighModelIDs = []string{
		"gpt-5.2", "gpt-5.2-codex",
	}
)

// matchesExactOrPrefix checks if a model ID exactly matches or is a prefix match
// for any of the candidate IDs.
func matchesExactOrPrefix(modelID string, ids []string) bool {
	for _, candidate := range ids {
		if modelID == candidate || strings.HasPrefix(modelID, candidate+"-") {
			return true
		}
	}
	return false
}

// SupportsBuiltInXHighThinking checks if a provider/model combo supports xhigh reasoning.
//
// Mirrors src/auto-reply/thinking.shared.ts supportsBuiltInXHighThinking().
func SupportsBuiltInXHighThinking(provider, model string) bool {
	providerID := NormalizeProviderId(provider)
	modelID := strings.ToLower(strings.TrimSpace(model))
	if providerID == "" || modelID == "" {
		return false
	}
	switch providerID {
	case "openai":
		return matchesExactOrPrefix(modelID, openaiXHighModelIDs)
	case "openai-codex":
		return matchesExactOrPrefix(modelID, openaiCodexXHighModelIDs)
	case "github-copilot":
		for _, id := range githubCopilotXHighModelIDs {
			if modelID == id {
				return true
			}
		}
	}
	return false
}

// ListThinkingLevels returns available thinking levels, optionally including xhigh.
func ListThinkingLevels(provider, model string) []ThinkLevel {
	levels := make([]ThinkLevel, len(BaseThinkingLevels()))
	copy(levels, BaseThinkingLevels())
	if SupportsBuiltInXHighThinking(provider, model) {
		// Insert xhigh before the last element (adaptive).
		result := make([]ThinkLevel, 0, len(levels)+1)
		result = append(result, levels[:len(levels)-1]...)
		result = append(result, ThinkXHigh)
		result = append(result, levels[len(levels)-1])
		return result
	}
	return levels
}

// ListThinkingLevelLabelsWithModel returns labels considering both provider and model.
func ListThinkingLevelLabelsWithModel(provider, model string) []string {
	if IsBinaryThinkingProvider(provider) {
		return []string{"off", "on"}
	}
	levels := ListThinkingLevels(provider, model)
	result := make([]string, len(levels))
	for i, l := range levels {
		result[i] = string(l)
	}
	return result
}

// FormatThinkingLevelsWithModel returns a joined string of thinking levels.
func FormatThinkingLevelsWithModel(provider, model, separator string) string {
	if separator == "" {
		separator = ", "
	}
	return strings.Join(ListThinkingLevelLabelsWithModel(provider, model), separator)
}

// FormatXHighModelHint returns a hint string about xhigh support.
func FormatXHighModelHint() string {
	return "provider models that advertise xhigh reasoning"
}

// ResolveThinkingDefaultForModel determines the default thinking level for a
// provider/model combination.
//
// Mirrors src/auto-reply/thinking.shared.ts resolveThinkingDefaultForModel().
func ResolveThinkingDefaultForModel(provider, model string, catalog []ThinkingCatalogEntry) ThinkLevel {
	normalizedProvider := NormalizeProviderId(provider)
	modelID := strings.TrimSpace(model)

	// Anthropic Claude 4.6+ defaults to adaptive.
	if normalizedProvider == "anthropic" && anthropicClaude46ModelRe.MatchString(modelID) {
		return ThinkAdaptive
	}
	// Amazon Bedrock Claude 4.6+ defaults to adaptive.
	if normalizedProvider == "amazon-bedrock" && amazonBedrockClaude46ModelRe.MatchString(modelID) {
		return ThinkAdaptive
	}

	// Check catalog for reasoning flag.
	for _, entry := range catalog {
		if entry.Provider == provider && entry.ID == model && entry.Reasoning {
			return ThinkLow
		}
	}

	return ThinkOff
}

// ResolveResponseUsageMode resolves the usage display mode, defaulting to "off".
func ResolveResponseUsageMode(raw string) UsageDisplayLevel {
	level, ok := NormalizeUsageDisplay(raw)
	if !ok {
		return UsageOff
	}
	return level
}
