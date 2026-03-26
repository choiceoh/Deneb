// types_provider.go — Provider plugin types for the Go gateway.
// Mirrors src/plugins/types-provider.ts (754 LOC).
//
// Defines the ProviderPlugin interface, auth types, catalog types,
// wizard configuration, and all provider hook context/result types.
package plugin

import "context"

// --- Auth types ---

// ProviderAuthKind identifies the authentication mechanism.
type ProviderAuthKind string

const (
	AuthKindOAuth      ProviderAuthKind = "oauth"
	AuthKindAPIKey     ProviderAuthKind = "api_key"
	AuthKindToken      ProviderAuthKind = "token"
	AuthKindDeviceCode ProviderAuthKind = "device_code"
	AuthKindCustom     ProviderAuthKind = "custom"
)

// ProviderAuthMethod describes one authentication method for a provider.
type ProviderAuthMethod struct {
	ID     string           `json:"id"`
	Label  string           `json:"label"`
	Hint   string           `json:"hint,omitempty"`
	Kind   ProviderAuthKind `json:"kind"`
	Wizard *ProviderPluginWizardSetup `json:"wizard,omitempty"`
}

// ProviderAuthResult is the result of a provider auth flow.
type ProviderAuthResult struct {
	Profiles     []AuthProfileEntry     `json:"profiles"`
	ConfigPatch  map[string]any         `json:"configPatch,omitempty"`
	DefaultModel string                 `json:"defaultModel,omitempty"`
	Notes        []string               `json:"notes,omitempty"`
}

// AuthProfileEntry associates a profile ID with a credential.
type AuthProfileEntry struct {
	ProfileID  string         `json:"profileId"`
	Credential map[string]any `json:"credential"`
}

// --- Catalog types ---

// ProviderCatalogOrder determines the order in which provider catalogs
// are resolved during discovery.
type ProviderCatalogOrder string

const (
	CatalogOrderSimple  ProviderCatalogOrder = "simple"
	CatalogOrderProfile ProviderCatalogOrder = "profile"
	CatalogOrderPaired  ProviderCatalogOrder = "paired"
	CatalogOrderLate    ProviderCatalogOrder = "late"
)

// AllCatalogOrders is the ordered list of catalog discovery phases.
var AllCatalogOrders = []ProviderCatalogOrder{
	CatalogOrderSimple,
	CatalogOrderProfile,
	CatalogOrderPaired,
	CatalogOrderLate,
}

// ProviderCatalogContext provides context for catalog discovery.
type ProviderCatalogContext struct {
	Config       map[string]any    `json:"config,omitempty"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env,omitempty"`

	// ResolveProviderApiKey returns the API key for a provider ID.
	ResolveProviderApiKey func(providerID string) *ResolvedApiKey `json:"-"`
	// ResolveProviderAuth returns full auth info for a provider ID.
	ResolveProviderAuth func(providerID string, opts *ProviderAuthOpts) *ResolvedProviderAuth `json:"-"`
}

// ResolvedApiKey holds the result of an API key lookup.
type ResolvedApiKey struct {
	ApiKey       string `json:"apiKey,omitempty"`
	DiscoveryKey string `json:"discoveryApiKey,omitempty"`
}

// ProviderAuthOpts holds options for provider auth resolution.
type ProviderAuthOpts struct {
	OAuthMarker string `json:"oauthMarker,omitempty"`
}

// ResolvedProviderAuth holds full auth resolution results.
type ResolvedProviderAuth struct {
	ApiKey       string `json:"apiKey,omitempty"`
	DiscoveryKey string `json:"discoveryApiKey,omitempty"`
	Mode         string `json:"mode"`      // "api_key", "oauth", "token", "none"
	Source       string `json:"source"`    // "env", "profile", "none"
	ProfileID    string `json:"profileId,omitempty"`
}

// ModelProviderConfig holds configuration for a model provider.
// Simplified from the TS ModelProviderConfig.
type ModelProviderConfig struct {
	ID      string         `json:"id,omitempty"`
	BaseURL string         `json:"baseUrl,omitempty"`
	ApiKey  string         `json:"apiKey,omitempty"`
	API     string         `json:"api,omitempty"`
	Models  map[string]any `json:"models,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// ProviderCatalogResult is the result of a catalog discovery call.
// Either Provider or Providers is set (never both).
type ProviderCatalogResult struct {
	Provider  *ModelProviderConfig            `json:"provider,omitempty"`
	Providers map[string]*ModelProviderConfig `json:"providers,omitempty"`
}

// ProviderPluginCatalog describes a provider's catalog hook.
type ProviderPluginCatalog struct {
	Order ProviderCatalogOrder `json:"order,omitempty"`
	Run   func(ctx context.Context, cctx ProviderCatalogContext) (*ProviderCatalogResult, error) `json:"-"`
}

// --- Runtime model types ---

// ProviderRuntimeModel represents a resolved model ready for inference.
type ProviderRuntimeModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	BaseURL  string `json:"baseUrl,omitempty"`
	APIType  string `json:"apiType,omitempty"`
}

// ProviderRuntimeProviderConfig holds runtime provider configuration.
type ProviderRuntimeProviderConfig struct {
	BaseURL string         `json:"baseUrl,omitempty"`
	API     string         `json:"api,omitempty"`
	Models  map[string]any `json:"models,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// --- Hook context types ---

// ProviderResolveDynamicModelContext provides context for dynamic model resolution.
type ProviderResolveDynamicModelContext struct {
	Config         map[string]any                 `json:"config,omitempty"`
	AgentDir       string                         `json:"agentDir,omitempty"`
	WorkspaceDir   string                         `json:"workspaceDir,omitempty"`
	Provider       string                         `json:"provider"`
	ModelID        string                         `json:"modelId"`
	ProviderConfig *ProviderRuntimeProviderConfig `json:"providerConfig,omitempty"`
}

// ProviderPrepareDynamicModelContext provides context for async dynamic model warm-up.
type ProviderPrepareDynamicModelContext = ProviderResolveDynamicModelContext

// ProviderNormalizeResolvedModelContext provides context for model normalization.
type ProviderNormalizeResolvedModelContext struct {
	Config       map[string]any        `json:"config,omitempty"`
	AgentDir     string                `json:"agentDir,omitempty"`
	WorkspaceDir string                `json:"workspaceDir,omitempty"`
	Provider     string                `json:"provider"`
	ModelID      string                `json:"modelId"`
	Model        ProviderRuntimeModel  `json:"model"`
}

// ProviderPrepareRuntimeAuthContext provides context for runtime auth exchange.
type ProviderPrepareRuntimeAuthContext struct {
	Config       map[string]any       `json:"config,omitempty"`
	AgentDir     string               `json:"agentDir,omitempty"`
	WorkspaceDir string               `json:"workspaceDir,omitempty"`
	Env          map[string]string    `json:"env,omitempty"`
	Provider     string               `json:"provider"`
	ModelID      string               `json:"modelId"`
	Model        ProviderRuntimeModel `json:"model"`
	ApiKey       string               `json:"apiKey"`
	AuthMode     string               `json:"authMode"`
	ProfileID    string               `json:"profileId,omitempty"`
}

// ProviderPreparedRuntimeAuth is the result of runtime auth exchange.
type ProviderPreparedRuntimeAuth struct {
	ApiKey    string `json:"apiKey"`
	BaseURL   string `json:"baseUrl,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

// ProviderResolveUsageAuthContext provides context for usage auth resolution.
type ProviderResolveUsageAuthContext struct {
	Config       map[string]any    `json:"config"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env"`
	Provider     string            `json:"provider"`
}

// ProviderResolvedUsageAuth is the result of usage auth resolution.
type ProviderResolvedUsageAuth struct {
	Token     string `json:"token"`
	AccountID string `json:"accountId,omitempty"`
}

// ProviderFetchUsageSnapshotContext provides context for usage snapshot fetch.
type ProviderFetchUsageSnapshotContext struct {
	Config       map[string]any    `json:"config"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env"`
	Provider     string            `json:"provider"`
	Token        string            `json:"token"`
	AccountID    string            `json:"accountId,omitempty"`
	TimeoutMs    int64             `json:"timeoutMs"`
}

// ProviderPrepareExtraParamsContext provides context for extra param normalization.
type ProviderPrepareExtraParamsContext struct {
	Config       map[string]any `json:"config,omitempty"`
	AgentDir     string         `json:"agentDir,omitempty"`
	WorkspaceDir string         `json:"workspaceDir,omitempty"`
	Provider     string         `json:"provider"`
	ModelID      string         `json:"modelId"`
	ExtraParams  map[string]any `json:"extraParams,omitempty"`
	ThinkingLevel string        `json:"thinkingLevel,omitempty"`
}

// ProviderCacheTtlEligibilityContext provides context for cache TTL eligibility.
type ProviderCacheTtlEligibilityContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ProviderThinkingPolicyContext provides context for thinking policy hooks.
type ProviderThinkingPolicyContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ProviderDefaultThinkingPolicyContext extends thinking policy with reasoning hint.
type ProviderDefaultThinkingPolicyContext struct {
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

// ProviderModernModelPolicyContext provides context for "modern model" checks.
type ProviderModernModelPolicyContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ProviderBuiltInModelSuppressionContext provides context for model suppression.
type ProviderBuiltInModelSuppressionContext struct {
	Config       map[string]any    `json:"config,omitempty"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env"`
	Provider     string            `json:"provider"`
	ModelID      string            `json:"modelId"`
}

// ProviderBuiltInModelSuppressionResult is the result of model suppression.
type ProviderBuiltInModelSuppressionResult struct {
	Suppress     bool   `json:"suppress"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ProviderBuildMissingAuthMessageContext provides context for missing auth messages.
type ProviderBuildMissingAuthMessageContext struct {
	Config       map[string]any    `json:"config,omitempty"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env"`
	Provider     string            `json:"provider"`
}

// ProviderAuthDoctorHintContext provides context for auth doctor hints.
type ProviderAuthDoctorHintContext struct {
	Config    map[string]any `json:"config,omitempty"`
	Provider  string         `json:"provider"`
	ProfileID string         `json:"profileId,omitempty"`
}

// ModelCatalogEntry represents a model in the catalog.
type ModelCatalogEntry struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	Label         string `json:"label,omitempty"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	APIType       string `json:"apiType,omitempty"`
}

// ProviderAugmentModelCatalogContext provides context for catalog augmentation.
type ProviderAugmentModelCatalogContext struct {
	Config       map[string]any    `json:"config,omitempty"`
	AgentDir     string            `json:"agentDir,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Env          map[string]string `json:"env"`
	Entries      []ModelCatalogEntry `json:"entries"`
}

// --- Wizard types ---

// ProviderPluginWizardSetup describes wizard/onboarding setup metadata.
type ProviderPluginWizardSetup struct {
	ChoiceID       string                    `json:"choiceId,omitempty"`
	ChoiceLabel    string                    `json:"choiceLabel,omitempty"`
	ChoiceHint     string                    `json:"choiceHint,omitempty"`
	GroupID        string                    `json:"groupId,omitempty"`
	GroupLabel     string                    `json:"groupLabel,omitempty"`
	GroupHint      string                    `json:"groupHint,omitempty"`
	MethodID       string                    `json:"methodId,omitempty"`
	ModelAllowlist *ProviderModelAllowlist   `json:"modelAllowlist,omitempty"`
}

// ProviderModelAllowlist describes model filtering for wizard flows.
type ProviderModelAllowlist struct {
	AllowedKeys       []string `json:"allowedKeys,omitempty"`
	InitialSelections []string `json:"initialSelections,omitempty"`
	Message           string   `json:"message,omitempty"`
}

// ProviderPluginWizardModelPicker describes a model picker in wizards.
type ProviderPluginWizardModelPicker struct {
	Label    string `json:"label,omitempty"`
	Hint     string `json:"hint,omitempty"`
	MethodID string `json:"methodId,omitempty"`
}

// ProviderPluginWizard holds wizard configuration for a provider.
type ProviderPluginWizard struct {
	Setup       *ProviderPluginWizardSetup       `json:"setup,omitempty"`
	ModelPicker *ProviderPluginWizardModelPicker  `json:"modelPicker,omitempty"`
}

// --- ProviderPlugin: the main provider plugin interface ---

// ProviderPlugin describes a registered provider plugin with all hooks.
// This is the Go equivalent of the TS ProviderPlugin type.
type ProviderPlugin struct {
	ID       string `json:"id"`
	PluginID string `json:"pluginId,omitempty"`
	Label    string `json:"label"`
	DocsPath string `json:"docsPath,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	EnvVars  []string `json:"envVars,omitempty"`
	DeprecatedProfileIds []string `json:"deprecatedProfileIds,omitempty"`
	Auth     []ProviderAuthMethod `json:"auth"`
	Wizard   *ProviderPluginWizard `json:"wizard,omitempty"`

	// Catalog hook for provider model discovery.
	Catalog *ProviderPluginCatalog `json:"-"`
	// Legacy alias for Catalog.
	Discovery *ProviderPluginCatalog `json:"-"`

	// Provider capabilities (static feature flags).
	Capabilities map[string]any `json:"capabilities,omitempty"`

	// --- Optional hook functions ---

	// ResolveDynamicModel resolves model IDs not in the static catalog.
	ResolveDynamicModel func(ctx ProviderResolveDynamicModelContext) *ProviderRuntimeModel `json:"-"`
	// PrepareDynamicModel is an async warm-up for dynamic model resolution.
	PrepareDynamicModel func(ctx context.Context, dctx ProviderPrepareDynamicModelContext) error `json:"-"`
	// NormalizeResolvedModel rewrites resolved models before inference.
	NormalizeResolvedModel func(ctx ProviderNormalizeResolvedModelContext) *ProviderRuntimeModel `json:"-"`
	// PrepareExtraParams normalizes extra params before stream wrapping.
	PrepareExtraParams func(ctx ProviderPrepareExtraParamsContext) map[string]any `json:"-"`
	// PrepareRuntimeAuth exchanges a stored credential for a runtime token.
	PrepareRuntimeAuth func(ctx context.Context, actx ProviderPrepareRuntimeAuthContext) (*ProviderPreparedRuntimeAuth, error) `json:"-"`
	// ResolveUsageAuth resolves usage/billing auth credentials.
	ResolveUsageAuth func(ctx context.Context, uctx ProviderResolveUsageAuthContext) (*ProviderResolvedUsageAuth, error) `json:"-"`
	// FetchUsageSnapshot fetches provider-specific usage data.
	FetchUsageSnapshot func(ctx context.Context, fctx ProviderFetchUsageSnapshotContext) (map[string]any, error) `json:"-"`
	// IsCacheTtlEligible returns cache TTL eligibility for a model.
	IsCacheTtlEligible func(ctx ProviderCacheTtlEligibilityContext) *bool `json:"-"`
	// IsBinaryThinking returns whether the provider uses binary thinking.
	IsBinaryThinking func(ctx ProviderThinkingPolicyContext) *bool `json:"-"`
	// SupportsXHighThinking returns whether the provider supports xhigh.
	SupportsXHighThinking func(ctx ProviderThinkingPolicyContext) *bool `json:"-"`
	// ResolveDefaultThinkingLevel returns the default thinking level.
	ResolveDefaultThinkingLevel func(ctx ProviderDefaultThinkingPolicyContext) string `json:"-"`
	// IsModernModelRef returns whether a model is a "modern" reference.
	IsModernModelRef func(ctx ProviderModernModelPolicyContext) *bool `json:"-"`
	// SuppressBuiltInModel returns whether a built-in model should be hidden.
	SuppressBuiltInModel func(ctx ProviderBuiltInModelSuppressionContext) *ProviderBuiltInModelSuppressionResult `json:"-"`
	// AugmentModelCatalog returns extra catalog entries to append.
	AugmentModelCatalog func(ctx context.Context, actx ProviderAugmentModelCatalogContext) ([]ModelCatalogEntry, error) `json:"-"`
	// BuildMissingAuthMessage returns a custom missing-auth message.
	BuildMissingAuthMessage func(ctx ProviderBuildMissingAuthMessageContext) string `json:"-"`
	// BuildAuthDoctorHint returns a repair hint for auth failures.
	BuildAuthDoctorHint func(ctx context.Context, hctx ProviderAuthDoctorHintContext) (string, error) `json:"-"`
	// FormatApiKey formats an auth profile credential into an API key string.
	FormatApiKey func(cred map[string]any) string `json:"-"`
}

// ResolveProviderCatalogHook returns the active catalog hook (catalog or legacy discovery).
func (p *ProviderPlugin) ResolveProviderCatalogHook() *ProviderPluginCatalog {
	if p.Catalog != nil {
		return p.Catalog
	}
	return p.Discovery
}

// HasCatalogHook returns true if the provider has a catalog or discovery hook.
func (p *ProviderPlugin) HasCatalogHook() bool {
	return p.ResolveProviderCatalogHook() != nil
}
