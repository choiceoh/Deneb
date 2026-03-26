package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
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
	providerID := types.NormalizeProviderId(provider)
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
func ListThinkingLevels(provider, model string) []types.ThinkLevel {
	levels := make([]types.ThinkLevel, len(types.BaseThinkingLevels()))
	copy(levels, types.BaseThinkingLevels())
	if SupportsBuiltInXHighThinking(provider, model) {
		// Insert xhigh before the last element (adaptive).
		result := make([]types.ThinkLevel, 0, len(levels)+1)
		result = append(result, levels[:len(levels)-1]...)
		result = append(result, types.ThinkXHigh)
		result = append(result, levels[len(levels)-1])
		return result
	}
	return levels
}

// ListThinkingLevelLabelsWithModel returns labels considering both provider and model.
func ListThinkingLevelLabelsWithModel(provider, model string) []string {
	if types.IsBinaryThinkingProvider(provider) {
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
func ResolveThinkingDefaultForModel(provider, model string, catalog []ThinkingCatalogEntry) types.ThinkLevel {
	normalizedProvider := types.NormalizeProviderId(provider)
	modelID := strings.TrimSpace(model)

	// Anthropic Claude 4.6+ defaults to adaptive.
	if normalizedProvider == "anthropic" && anthropicClaude46ModelRe.MatchString(modelID) {
		return types.ThinkAdaptive
	}
	// Amazon Bedrock Claude 4.6+ defaults to adaptive.
	if normalizedProvider == "amazon-bedrock" && amazonBedrockClaude46ModelRe.MatchString(modelID) {
		return types.ThinkAdaptive
	}

	// Check catalog for reasoning flag.
	for _, entry := range catalog {
		if entry.Provider == provider && entry.ID == model && entry.Reasoning {
			return types.ThinkLow
		}
	}

	return types.ThinkOff
}

// ResolveResponseUsageMode resolves the usage display mode, defaulting to "off".
func ResolveResponseUsageMode(raw string) types.UsageDisplayLevel {
	level, ok := types.NormalizeUsageDisplay(raw)
	if !ok {
		return types.UsageOff
	}
	return level
}
