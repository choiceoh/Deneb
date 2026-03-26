// runtime.go — Provider runtime resolution for the Go gateway.
// Mirrors src/plugins/provider-runtime.ts (500 LOC).
//
// Resolves provider plugins and dispatches all provider hook calls
// (dynamic model, normalize, capabilities, extra params, auth, thinking
// policy, model suppression, catalog augmentation, etc.).
//
// Uses the provider.Registry to find plugins by normalized ID and delegates
// hook calls to the matching plugin.
package provider

import (
	"context"
	"log/slog"
	"sync"
)

// ProviderRuntimeResolver resolves provider plugins and dispatches hook calls.
// This is the Go equivalent of the provider-runtime.ts module.
type ProviderRuntimeResolver struct {
	mu       sync.RWMutex
	registry *Registry
	cache    map[string]Plugin // cached lookups by normalized provider ID
	logger   *slog.Logger
}

// NewProviderRuntimeResolver creates a new provider runtime resolver.
func NewProviderRuntimeResolver(registry *Registry, logger *slog.Logger) *ProviderRuntimeResolver {
	return &ProviderRuntimeResolver{
		registry: registry,
		cache:    make(map[string]Plugin),
		logger:   logger,
	}
}

// ResetCache clears the cached plugin lookups.
func (r *ProviderRuntimeResolver) ResetCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]Plugin)
}

// ResolvePlugin finds the provider plugin for the given provider ID.
// Uses normalized ID matching with alias support.
func (r *ProviderRuntimeResolver) ResolvePlugin(providerID string) Plugin {
	normalized := NormalizeProviderID(providerID)
	if normalized == "" {
		return nil
	}

	// Check cache first.
	r.mu.RLock()
	cached, ok := r.cache[normalized]
	r.mu.RUnlock()
	if ok {
		return cached
	}

	// Look up in registry using the normalized ID.
	plugin := r.registry.GetByNormalizedID(normalized)

	// Cache the result (including nil for negative caching).
	r.mu.Lock()
	r.cache[normalized] = plugin
	r.mu.Unlock()

	return plugin
}

// --- Dynamic model hooks ---

// ResolveDynamicModel calls the plugin's ResolveDynamicModel hook.
func (r *ProviderRuntimeResolver) ResolveDynamicModel(providerID string, dctx DynamicModelContext) *RuntimeModel {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if resolver, ok := plugin.(DynamicModelResolver); ok {
		model, err := resolver.ResolveDynamicModel(context.Background(), dctx)
		if err != nil {
			r.logger.Warn("resolveDynamicModel failed", "provider", providerID, "error", err)
			return nil
		}
		return model
	}
	return nil
}

// PrepareDynamicModel calls the plugin's async warm-up for dynamic models.
func (r *ProviderRuntimeResolver) PrepareDynamicModel(ctx context.Context, providerID string, dctx DynamicModelContext) error {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if preparer, ok := plugin.(DynamicModelPreparer); ok {
		return preparer.PrepareDynamicModel(ctx, dctx)
	}
	return nil
}

// --- Model normalization ---

// NormalizeResolvedModel calls the plugin's NormalizeResolvedModel hook.
func (r *ProviderRuntimeResolver) NormalizeResolvedModel(providerID string, nctx NormalizeContext) *RuntimeModel {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if normalizer, ok := plugin.(ModelNormalizer); ok {
		model, err := normalizer.NormalizeResolvedModel(context.Background(), nctx)
		if err != nil {
			r.logger.Warn("normalizeResolvedModel failed", "provider", providerID, "error", err)
			return nil
		}
		return model
	}
	return nil
}

// --- Capabilities ---

// ResolveCapabilities returns the plugin's static capabilities.
func (r *ProviderRuntimeResolver) ResolveCapabilities(providerID string) *Capabilities {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if cp, ok := plugin.(CapabilitiesProvider); ok {
		caps := cp.Capabilities()
		return &caps
	}
	return nil
}

// --- Runtime auth ---

// PrepareRuntimeAuth calls the plugin's runtime auth exchange hook.
func (r *ProviderRuntimeResolver) PrepareRuntimeAuth(ctx context.Context, providerID string, actx RuntimeAuthContext) (*PreparedAuth, error) {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil, nil
	}
	if rap, ok := plugin.(RuntimeAuthProvider); ok {
		return rap.PrepareRuntimeAuth(ctx, actx)
	}
	return nil, nil
}

// --- Extra params ---

// PrepareExtraParams calls the plugin's extra param normalization hook.
func (r *ProviderRuntimeResolver) PrepareExtraParams(providerID string, pctx ExtraParamsContext) map[string]any {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if ep, ok := plugin.(ExtraParamsProvider); ok {
		return ep.PrepareExtraParams(pctx)
	}
	return nil
}

// --- Thinking policy hooks ---

// IsBinaryThinking returns whether the provider uses binary thinking.
func (r *ProviderRuntimeResolver) IsBinaryThinking(providerID, modelID string) *bool {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if tp, ok := plugin.(ThinkingPolicyProvider); ok {
		return tp.IsBinaryThinking(ThinkingPolicyContext{Provider: providerID, ModelID: modelID})
	}
	return nil
}

// SupportsXHighThinking returns whether the provider supports xhigh thinking.
func (r *ProviderRuntimeResolver) SupportsXHighThinking(providerID, modelID string) *bool {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if tp, ok := plugin.(ThinkingPolicyProvider); ok {
		return tp.SupportsXHighThinking(ThinkingPolicyContext{Provider: providerID, ModelID: modelID})
	}
	return nil
}

// ResolveDefaultThinkingLevel returns the default thinking level for a model.
func (r *ProviderRuntimeResolver) ResolveDefaultThinkingLevel(providerID, modelID string, reasoning bool) string {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return ""
	}
	if tp, ok := plugin.(DefaultThinkingProvider); ok {
		return tp.ResolveDefaultThinkingLevel(DefaultThinkingContext{
			Provider: providerID, ModelID: modelID, Reasoning: reasoning,
		})
	}
	return ""
}

// IsModernModelRef returns whether a model is a "modern" reference.
func (r *ProviderRuntimeResolver) IsModernModelRef(providerID, modelID string) *bool {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if mp, ok := plugin.(ModernModelProvider); ok {
		return mp.IsModernModelRef(ModernModelContext{Provider: providerID, ModelID: modelID})
	}
	return nil
}

// --- Cache TTL eligibility ---

// IsCacheTtlEligible returns cache TTL eligibility for a model.
func (r *ProviderRuntimeResolver) IsCacheTtlEligible(providerID, modelID string) *bool {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if cp, ok := plugin.(CacheTtlProvider); ok {
		return cp.IsCacheTtlEligible(CacheTtlContext{Provider: providerID, ModelID: modelID})
	}
	return nil
}

// --- Model suppression ---

// ResolveBuiltInModelSuppression checks if a built-in model should be hidden.
func (r *ProviderRuntimeResolver) ResolveBuiltInModelSuppression(providerID, modelID string) *ModelSuppressionResult {
	// Check all registered plugins for suppression.
	snapshot := r.registry.Snapshot()
	for _, plugin := range snapshot {
		if sp, ok := plugin.(ModelSuppressionProvider); ok {
			result := sp.SuppressBuiltInModel(ModelSuppressionContext{
				Provider: providerID,
				ModelID:  modelID,
			})
			if result != nil && result.Suppress {
				return result
			}
		}
	}
	return nil
}

// --- Missing auth message ---

// BuildMissingAuthMessage returns a custom missing-auth message.
func (r *ProviderRuntimeResolver) BuildMissingAuthMessage(providerID string) string {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return ""
	}
	if mp, ok := plugin.(MissingAuthMessageProvider); ok {
		return mp.BuildMissingAuthMessage(MissingAuthContext{Provider: providerID})
	}
	return ""
}

// --- Catalog augmentation ---

// AugmentModelCatalog collects extra catalog entries from all provider plugins.
func (r *ProviderRuntimeResolver) AugmentModelCatalog(ctx context.Context, entries []CatalogEntry) []CatalogEntry {
	var supplemental []CatalogEntry
	snapshot := r.registry.Snapshot()
	for _, plugin := range snapshot {
		if ap, ok := plugin.(CatalogAugmenter); ok {
			extra, err := ap.AugmentModelCatalog(ctx, entries)
			if err != nil {
				r.logger.Warn("augmentModelCatalog failed", "plugin", plugin.ID(), "error", err)
				continue
			}
			supplemental = append(supplemental, extra...)
		}
	}
	return supplemental
}

// --- Usage auth ---

// ResolveUsageAuth resolves usage/billing auth credentials.
func (r *ProviderRuntimeResolver) ResolveUsageAuth(ctx context.Context, providerID string) (*ResolvedUsageAuth, error) {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil, nil
	}
	if up, ok := plugin.(UsageAuthProvider); ok {
		return up.ResolveUsageAuth(ctx, UsageAuthContext{Provider: providerID})
	}
	return nil, nil
}

// --- Format API key ---

// FormatApiKey formats an auth profile credential into an API key string.
func (r *ProviderRuntimeResolver) FormatApiKey(providerID string, cred map[string]any) string {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return ""
	}
	if fp, ok := plugin.(ApiKeyFormatter); ok {
		return fp.FormatApiKey(cred)
	}
	return ""
}

// --- Auth doctor hint ---

// BuildAuthDoctorHint returns a repair hint for auth failures.
func (r *ProviderRuntimeResolver) BuildAuthDoctorHint(ctx context.Context, providerID, profileID string) (string, error) {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return "", nil
	}
	if dp, ok := plugin.(AuthDoctorProvider); ok {
		return dp.BuildAuthDoctorHint(ctx, AuthDoctorContext{
			Provider:  providerID,
			ProfileID: profileID,
		})
	}
	return "", nil
}

// --- Optional adapter interfaces for runtime hooks ---

// DynamicModelPreparer provides async warm-up for dynamic model resolution.
type DynamicModelPreparer interface {
	PrepareDynamicModel(ctx context.Context, dctx DynamicModelContext) error
}

// ExtraParamsContext provides context for extra param normalization.
type ExtraParamsContext struct {
	Provider      string         `json:"provider"`
	ModelID       string         `json:"modelId"`
	ExtraParams   map[string]any `json:"extraParams,omitempty"`
	ThinkingLevel string         `json:"thinkingLevel,omitempty"`
}

// ExtraParamsProvider normalizes extra params before stream wrapping.
type ExtraParamsProvider interface {
	PrepareExtraParams(ctx ExtraParamsContext) map[string]any
}

// ThinkingPolicyContext provides context for thinking policy hooks.
type ThinkingPolicyContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ThinkingPolicyProvider reports thinking policy for a model.
type ThinkingPolicyProvider interface {
	IsBinaryThinking(ctx ThinkingPolicyContext) *bool
	SupportsXHighThinking(ctx ThinkingPolicyContext) *bool
}

// DefaultThinkingContext provides context for default thinking level.
type DefaultThinkingContext struct {
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

// DefaultThinkingProvider resolves the default thinking level.
type DefaultThinkingProvider interface {
	ResolveDefaultThinkingLevel(ctx DefaultThinkingContext) string
}

// ModernModelContext provides context for "modern model" checks.
type ModernModelContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ModernModelProvider reports whether a model is "modern".
type ModernModelProvider interface {
	IsModernModelRef(ctx ModernModelContext) *bool
}

// CacheTtlContext provides context for cache TTL eligibility.
type CacheTtlContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// CacheTtlProvider reports cache TTL eligibility.
type CacheTtlProvider interface {
	IsCacheTtlEligible(ctx CacheTtlContext) *bool
}

// ModelSuppressionContext provides context for model suppression.
type ModelSuppressionContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ModelSuppressionResult is the result of model suppression.
type ModelSuppressionResult struct {
	Suppress     bool   `json:"suppress"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ModelSuppressionProvider reports whether a built-in model should be hidden.
type ModelSuppressionProvider interface {
	SuppressBuiltInModel(ctx ModelSuppressionContext) *ModelSuppressionResult
}

// MissingAuthContext provides context for missing auth messages.
type MissingAuthContext struct {
	Provider string `json:"provider"`
}

// MissingAuthMessageProvider returns a custom missing-auth message.
type MissingAuthMessageProvider interface {
	BuildMissingAuthMessage(ctx MissingAuthContext) string
}

// CatalogAugmenter provides extra catalog entries.
type CatalogAugmenter interface {
	AugmentModelCatalog(ctx context.Context, entries []CatalogEntry) ([]CatalogEntry, error)
}

// ResolvedUsageAuth is the result of usage auth resolution.
type ResolvedUsageAuth struct {
	Token     string `json:"token"`
	AccountID string `json:"accountId,omitempty"`
}

// UsageAuthContext provides context for usage auth resolution.
type UsageAuthContext struct {
	Provider string `json:"provider"`
}

// UsageAuthProvider resolves usage/billing auth credentials.
type UsageAuthProvider interface {
	ResolveUsageAuth(ctx context.Context, uctx UsageAuthContext) (*ResolvedUsageAuth, error)
}

// ApiKeyFormatter formats an auth profile credential into an API key.
type ApiKeyFormatter interface {
	FormatApiKey(cred map[string]any) string
}

// AuthDoctorContext provides context for auth doctor hints.
type AuthDoctorContext struct {
	Provider  string `json:"provider"`
	ProfileID string `json:"profileId,omitempty"`
}

// AuthDoctorProvider returns repair hints for auth failures.
type AuthDoctorProvider interface {
	BuildAuthDoctorHint(ctx context.Context, dctx AuthDoctorContext) (string, error)
}


