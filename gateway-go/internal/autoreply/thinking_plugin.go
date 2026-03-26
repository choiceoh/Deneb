package autoreply

import (
	"sync"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ProviderThinkingContext provides context for plugin-based thinking level resolution.
type ProviderThinkingContext struct {
	Provider  string
	ModelID   string
	Reasoning bool // from catalog entry
}

// ProviderThinkingResolver is the callback interface for plugin-based thinking
// level overrides. Each function returns a value and a boolean indicating whether
// the plugin provided a decision.
//
// Mirrors src/plugins/provider-runtime.ts:
// - resolveProviderBinaryThinking
// - resolveProviderXHighThinking
// - resolveProviderDefaultThinkingLevel
type ProviderThinkingResolver interface {
	// ResolveBinaryThinking returns whether the provider only supports on/off thinking.
	// Returns (decision, hasDecision).
	ResolveBinaryThinking(provider string, ctx ProviderThinkingContext) (bool, bool)

	// ResolveXHighThinking returns whether the provider/model supports xhigh thinking.
	// Returns (decision, hasDecision).
	ResolveXHighThinking(provider string, ctx ProviderThinkingContext) (bool, bool)

	// ResolveDefaultThinkingLevel returns the provider's default thinking level.
	// Returns (level, hasDecision).
	ResolveDefaultThinkingLevel(provider string, ctx ProviderThinkingContext) (types.ThinkLevel, bool)
}

// ThinkingRuntime wraps the shared thinking functions with plugin-aware overrides.
// When a ProviderThinkingResolver is registered, its decisions take priority
// over the built-in logic.
//
// Mirrors the wrapper pattern in src/auto-reply/thinking.ts.
type ThinkingRuntime struct {
	mu       sync.RWMutex
	resolver ProviderThinkingResolver
}

// NewThinkingRuntime creates a new thinking runtime.
func NewThinkingRuntime() *ThinkingRuntime {
	return &ThinkingRuntime{}
}

// SetResolver registers a plugin-based thinking resolver.
func (r *ThinkingRuntime) SetResolver(resolver ProviderThinkingResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolver = resolver
}

func (r *ThinkingRuntime) getResolver() ProviderThinkingResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolver
}

// IsBinaryThinkingProvider checks if a provider only supports on/off thinking.
// Consults plugin resolver first, then falls back to built-in check.
func (r *ThinkingRuntime) IsBinaryThinkingProvider(provider, model string) bool {
	// Built-in check first.
	if types.IsBinaryThinkingProvider(provider) {
		return true
	}

	resolver := r.getResolver()
	if resolver == nil {
		return false
	}

	normalizedProvider := types.NormalizeProviderId(provider)
	if normalizedProvider == "" {
		return false
	}

	decision, ok := resolver.ResolveBinaryThinking(normalizedProvider, ProviderThinkingContext{
		Provider: normalizedProvider,
		ModelID:  trimModel(model),
	})
	if ok {
		return decision
	}
	return false
}

// SupportsXHighThinking checks if a provider/model supports xhigh thinking.
// Consults built-in check, then plugin resolver.
func (r *ThinkingRuntime) SupportsXHighThinking(provider, model string) bool {
	modelKey := trimModel(model)
	if modelKey == "" {
		return false
	}

	// Built-in check.
	if SupportsBuiltInXHighThinking(provider, modelKey) {
		return true
	}

	resolver := r.getResolver()
	if resolver == nil {
		return false
	}

	providerKey := types.NormalizeProviderId(provider)
	if providerKey == "" {
		return false
	}

	decision, ok := resolver.ResolveXHighThinking(providerKey, ProviderThinkingContext{
		Provider: providerKey,
		ModelID:  modelKey,
	})
	if ok {
		return decision
	}
	return false
}

// ListThinkingLevels returns available thinking levels, consulting plugin for xhigh support.
func (r *ThinkingRuntime) ListThinkingLevels(provider, model string) []types.ThinkLevel {
	levels := make([]types.ThinkLevel, len(types.BaseThinkingLevels()))
	copy(levels, types.BaseThinkingLevels())
	if r.SupportsXHighThinking(provider, model) {
		result := make([]types.ThinkLevel, 0, len(levels)+1)
		result = append(result, levels[:len(levels)-1]...)
		result = append(result, types.ThinkXHigh)
		result = append(result, levels[len(levels)-1])
		return result
	}
	return levels
}

// ListThinkingLevelLabels returns labels, using plugin-aware binary detection.
func (r *ThinkingRuntime) ListThinkingLevelLabels(provider, model string) []string {
	if r.IsBinaryThinkingProvider(provider, model) {
		return []string{"off", "on"}
	}
	levels := r.ListThinkingLevels(provider, model)
	result := make([]string, len(levels))
	for i, l := range levels {
		result[i] = string(l)
	}
	return result
}

// FormatThinkingLevels returns a joined string, plugin-aware.
func (r *ThinkingRuntime) FormatThinkingLevels(provider, model, separator string) string {
	if separator == "" {
		separator = ", "
	}
	return joinStrings(r.ListThinkingLevelLabels(provider, model), separator)
}

// ResolveThinkingDefaultForModel determines the default thinking level,
// consulting plugin resolver first.
func (r *ThinkingRuntime) ResolveThinkingDefaultForModel(provider, model string, catalog []ThinkingCatalogEntry) types.ThinkLevel {
	resolver := r.getResolver()
	if resolver != nil {
		normalizedProvider := types.NormalizeProviderId(provider)
		var reasoning bool
		for _, entry := range catalog {
			if entry.Provider == provider && entry.ID == model {
				reasoning = entry.Reasoning
				break
			}
		}
		level, ok := resolver.ResolveDefaultThinkingLevel(normalizedProvider, ProviderThinkingContext{
			Provider:  normalizedProvider,
			ModelID:   model,
			Reasoning: reasoning,
		})
		if ok {
			return level
		}
	}

	// Fall back to built-in logic.
	return ResolveThinkingDefaultForModel(provider, model, catalog)
}

func trimModel(model string) string {
	if model == "" {
		return ""
	}
	// Trim and lowercase.
	result := model
	start, end := 0, len(result)
	for start < end && (result[start] == ' ' || result[start] == '\t') {
		start++
	}
	for end > start && (result[end-1] == ' ' || result[end-1] == '\t') {
		end--
	}
	return result[start:end]
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
