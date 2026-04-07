package thinking

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"strings"
)

// ThinkingCatalogEntry represents a model entry in the thinking catalog.
type ThinkingCatalogEntry struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

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
	providerID := types.NormalizeProviderID(provider)
	modelID := strings.ToLower(strings.TrimSpace(model))
	if providerID == "" || modelID == "" {
		return false
	}
	if ids, ok := xhighThinkingModelIDs[providerID]; ok {
		return matchesExactOrPrefix(modelID, ids)
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
	normalizedProvider := types.NormalizeProviderID(provider)
	modelID := strings.TrimSpace(model)

	// Check adaptive thinking regex patterns (defined in model_caps.yaml).
	if re, ok := adaptiveThinkingModelRes[normalizedProvider]; ok && re.MatchString(modelID) {
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
