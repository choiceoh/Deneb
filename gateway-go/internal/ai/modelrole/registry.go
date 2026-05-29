// Package modelrole provides a centralized model role registry for the gateway.
//
// Four model roles are defined (main, lightweight, pilot, fallback), each with
// a provider, model name, base URL, and API type. Subsystems declare which ROLE
// they need (e.g., "lightweight"); the registry resolves the concrete model
// config and provides a cached LLM client.
//
// Fallback chains are automatic:
//
//	Main       → Lightweight → Fallback
//	Lightweight → Fallback
//	Fallback   → (none)
package modelrole

import (
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Role identifies a model purpose within the system.
type Role string

const (
	RoleMain        Role = "main"
	RoleLightweight Role = "lightweight"
	RoleFallback    Role = "fallback"
)

// ModelConfig holds the provider and endpoint settings for a single model role.
type ModelConfig struct {
	ProviderID string // e.g., "zai", "localai", "google"
	Model      string // model name sent to the API
	BaseURL    string // API endpoint URL
	APIKey     string // empty for keyless providers (e.g., local AI)
	APIMode    string // "openai" (default) or "anthropic"; routes to the matching LLM client
}

// ProviderResolved is a resolved entry from the deneb.json models.providers
// catalog. The caller (server package) converts its provider configs into
// this dependency-free shape so a role can target ANY configured provider —
// not just the built-in resolveBaseURL switch.
type ProviderResolved struct {
	BaseURL string
	APIKey  string
	APIMode string // "openai" (default) or "anthropic"
}

// RegistryOptions configures NewRegistryWithOptions.
type RegistryOptions struct {
	MainModel        string // "provider/model"; empty → local vLLM
	LocalVllmModel   string // served model name for the local vLLM default
	LightweightModel string // override for RoleLightweight; empty → local vLLM
	FallbackModel    string // override for RoleFallback; empty → local vLLM
	// Providers is the deneb.json provider catalog (providerID → resolved
	// endpoint/credentials). A role whose provider is present here resolves
	// from the catalog; otherwise it falls back to the built-in switch.
	Providers map[string]ProviderResolved
}

// clientEntry caches a lazily-initialized LLM client per role.
type clientEntry struct {
	once   sync.Once
	client *llm.Client
}

// Registry holds the configured model roles and provides resolution,
// client caching, and fallback chain logic.
type Registry struct {
	mu        sync.RWMutex
	models    map[Role]ModelConfig
	clients   map[Role]*clientEntry
	providers map[string]ProviderResolved // deneb.json catalog, for runtime role re-resolution
	logger    *slog.Logger
}

// Default constants for known providers.
const (
	DefaultLocalAIBaseURL = "http://127.0.0.1:30000/v1"

	DefaultVllmBaseURL = "http://127.0.0.1:8000/v1"
	DefaultVllmModel   = "gemma4"

	DefaultZaiBaseURL = "https://api.z.ai/api/anthropic"
	DefaultZaiModel   = "glm-5-turbo"

	// Xiaomi MiMo — Anthropic-compatible endpoints. MiMo exposes both an
	// OpenAI-compatible (/v1) and an Anthropic Messages (/anthropic) API;
	// the gateway speaks Anthropic so prompt caching and extended thinking
	// work end-to-end.
	//
	// DefaultMimoBaseURL is the global standard API. DefaultMimoPlanBaseURL
	// is the Token Plan subscription endpoint, which is region-specific
	// (token-plan-sgp / -cn / -ams) — Singapore is the default. Operators
	// in another region override `baseUrl` in deneb.json.
	DefaultMimoBaseURL     = "https://api.xiaomimimo.com/anthropic"
	DefaultMimoPlanBaseURL = "https://token-plan-sgp.xiaomimimo.com/anthropic"

	// Kimi Code — Moonshot AI's coding subscription, served from its
	// dedicated coding endpoint (distinct from the general api.moonshot.ai
	// API). Anthropic-compatible so prompt caching and extended thinking
	// work end-to-end. The subscription authenticates with an OAuth token
	// (the official Kimi CLI caches it after `/login`) sent as a Bearer
	// credential. Token Plan model ID: `kimi-for-coding`.
	DefaultKimiBaseURL = "https://api.kimi.com/coding"

	// codingAgentUserAgent is the default User-Agent for coding-subscription
	// providers (Kimi Code, MiMo Token Plan). Their endpoints only serve
	// recognized coding agents and reject the gateway's own identifier, so
	// these providers need a coding-agent User-Agent to function at all.
	// The version segment is matched loosely (by prefix) upstream; if the
	// expected value ever drifts, override it per provider via `headers` in
	// deneb.json without a rebuild.
	codingAgentUserAgent = "claude-code/2.1.142"
)

// NewRegistry creates a registry with the legacy two-argument signature
// (main model + local vLLM model), leaving lightweight/fallback on the local
// vLLM default. Prefer NewRegistryWithOptions for per-role configuration.
func NewRegistry(logger *slog.Logger, mainModel, localVllmModel string) *Registry {
	return NewRegistryWithOptions(logger, RegistryOptions{
		MainModel:      mainModel,
		LocalVllmModel: localVllmModel,
	})
}

// NewRegistryWithOptions builds the role registry from explicit per-role
// overrides and an optional provider catalog. Unset lightweight/fallback
// roles keep the built-in local vLLM default, preserving prior behaviour.
// mainModel/lightweight/fallback are "provider/model" IDs resolved against
// the catalog first, then the built-in provider switch.
func NewRegistryWithOptions(logger *slog.Logger, opts RegistryOptions) *Registry {
	if logger == nil {
		logger = slog.Default()
	}

	localVllmModel := opts.LocalVllmModel
	if localVllmModel == "" {
		localVllmModel = DefaultVllmModel
	}

	mainModel := opts.MainModel
	if mainModel == "" {
		// Fall back to local vLLM model when no main model is configured.
		mainModel = "vllm/" + localVllmModel
	}

	// Local vLLM default shared by any unconfigured lightweight/fallback role.
	vllmDefault := ModelConfig{
		ProviderID: "vllm",
		Model:      localVllmModel,
		BaseURL:    DefaultVllmBaseURL,
		APIKey:     resolveVllmAPIKey(),
	}

	models := map[Role]ModelConfig{
		RoleMain:        resolveModelConfig(mainModel, opts.Providers),
		RoleLightweight: vllmDefault,
		RoleFallback:    vllmDefault,
	}
	if opts.LightweightModel != "" {
		models[RoleLightweight] = resolveModelConfig(opts.LightweightModel, opts.Providers)
	}
	if opts.FallbackModel != "" {
		models[RoleFallback] = resolveModelConfig(opts.FallbackModel, opts.Providers)
	}

	// Auto-discover the actual model name the local vLLM is serving and
	// substitute it in when config drifts. reconcileVllmModel is a no-op for
	// non-vllm roles, so running it across all roles is safe.
	for _, role := range []Role{RoleMain, RoleLightweight, RoleFallback} {
		cfg := models[role]
		reconcileVllmModel(logger, &cfg)
		models[role] = cfg
	}

	r := &Registry{
		models:    models,
		clients:   make(map[Role]*clientEntry),
		providers: opts.Providers,
		logger:    logger,
	}

	// Pre-create client entries for lazy initialization.
	for role := range models {
		r.clients[role] = &clientEntry{}
	}

	logger.Info("modelrole: registry initialized",
		"main", logModelAlias(models[RoleMain]),
		"lightweight", logModelAlias(models[RoleLightweight]),
		"fallback", logModelAlias(models[RoleFallback]),
	)

	return r
}

// resolveModelConfig builds a ModelConfig for a "provider/model" ID,
// resolving endpoint + credentials from the provider catalog first and the
// built-in provider switch second. This lets a role target any provider
// configured in deneb.json (e.g. "google/...") instead of silently falling
// back to the zai default when the provider is not in the hardcoded switch.
func resolveModelConfig(modelID string, providers map[string]ProviderResolved) ModelConfig {
	providerID, modelName := ParseModelID(modelID)
	cfg := ModelConfig{ProviderID: providerID, Model: modelName}
	if p, ok := providers[providerID]; ok {
		cfg.BaseURL = p.BaseURL
		cfg.APIKey = p.APIKey
		cfg.APIMode = p.APIMode
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = resolveBaseURL(providerID)
	}
	if cfg.APIKey == "" {
		cfg.APIKey = resolveAPIKey(providerID)
	}
	if cfg.APIMode == "" {
		cfg.APIMode = resolveAPIMode(providerID)
	}
	return cfg
}

// Config returns the model configuration for the given role.
func (r *Registry) Config(role Role) ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.models[role]
}

// Model returns the model name for the given role.
func (r *Registry) Model(role Role) string {
	return r.Config(role).Model
}

// FullModelID returns "provider/model" for the given role.
func (r *Registry) FullModelID(role Role) string {
	cfg := r.Config(role)
	if cfg.ProviderID == "" {
		return cfg.Model
	}
	return cfg.ProviderID + "/" + cfg.Model
}

// BaseURL returns the base URL for the given role.
func (r *Registry) BaseURL(role Role) string {
	return r.Config(role).BaseURL
}

// Client returns a cached LLM client for the given role.
// The client is lazily created on first access and reused thereafter.
func (r *Registry) Client(role Role) *llm.Client {
	r.mu.RLock()
	entry, ok := r.clients[role]
	cfg := r.models[role]
	r.mu.RUnlock()

	if !ok {
		return nil
	}

	entry.once.Do(func() {
		entry.client = buildClient(r.logger, cfg)
	})
	return entry.client
}

// buildClient assembles an LLM client from a resolved ModelConfig,
// applying provider-specific headers, auth scheme, and (for Kimi Code)
// the per-request token callback. Shared by role clients and on-demand
// provider clients so both stay consistent.
func buildClient(logger *slog.Logger, cfg ModelConfig) *llm.Client {
	opts := []llm.ClientOption{llm.WithLogger(logger)}
	if cfg.APIMode != "" {
		opts = append(opts, llm.WithAPIMode(cfg.APIMode))
	}
	if h := DefaultHeaders(cfg.ProviderID); len(h) > 0 {
		opts = append(opts, llm.WithHeaders(h))
	}
	if scheme := ResolveAuthScheme(cfg.ProviderID); scheme != "" {
		opts = append(opts, llm.WithAuthScheme(scheme))
	}
	// Kimi Code authenticates with the official Kimi CLI's OAuth token
	// cache; read it per request so a re-login is picked up live.
	if cfg.ProviderID == "kimi" {
		opts = append(opts, llm.WithAPIKeyFunc(kimiToken))
	}
	return llm.NewClient(cfg.BaseURL, cfg.APIKey, opts...)
}

// isBuiltinProvider reports whether providerID is one resolveBaseURL maps
// to a dedicated endpoint (as opposed to the zai fallback for unknown IDs).
func isBuiltinProvider(providerID string) bool {
	switch providerID {
	case "zai", "localai", "vllm", "openrouter", "mimo", "mimo-plan", "kimi":
		return true
	default:
		return false
	}
}

// ClientForProvider builds an LLM client for a known built-in provider,
// resolving its base URL, credential, API mode, auth scheme, and headers
// exactly as the role clients are built. Returns nil for an unknown
// provider.
//
// This satisfies /model switches to a provider that is not one of the
// three configured roles and has no deneb.json provider entry — e.g.
// switching to kimi from the quick-change keyboard when the startup main
// model is zai.
func (r *Registry) ClientForProvider(providerID string) *llm.Client {
	if !isBuiltinProvider(providerID) {
		return nil
	}
	return buildClient(r.logger, ModelConfig{
		ProviderID: providerID,
		BaseURL:    resolveBaseURL(providerID),
		APIKey:     resolveAPIKey(providerID),
		APIMode:    resolveAPIMode(providerID),
	})
}

// ResolveModel resolves a model string that may be a role name ("main", "lightweight",
// "fallback") into the actual full model ID. If the string is already a
// model name (not a role), it is returned unchanged along with ok=false.
// This allows callers to accept either role names or raw model names.
func (r *Registry) ResolveModel(modelOrRole string) (fullModelID string, role Role, ok bool) {
	switch Role(modelOrRole) {
	case RoleMain, RoleLightweight, RoleFallback:
		role = Role(modelOrRole)
		return r.FullModelID(role), role, true
	}
	return modelOrRole, "", false
}

// RoleForModel returns the role that matches the given full model ID (e.g., "google/gemini-3.1-pro").
// Returns ("", false) if no role matches.
func (r *Registry) RoleForModel(fullModelID string) (Role, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, role := range []Role{RoleMain, RoleLightweight, RoleFallback} {
		cfg, ok := r.models[role]
		if !ok {
			continue
		}
		fid := cfg.ProviderID + "/" + cfg.Model
		if cfg.ProviderID == "" {
			fid = cfg.Model
		}
		if fid == fullModelID {
			return role, true
		}
	}
	return "", false
}

// FallbackChain returns the ordered list of roles to try for the given role.
// The first element is always the role itself.
func (r *Registry) FallbackChain(role Role) []Role {
	switch role {
	case RoleMain:
		return []Role{RoleMain, RoleLightweight, RoleFallback}
	case RoleLightweight:
		return []Role{RoleLightweight, RoleFallback}
	case RoleFallback:
		return []Role{RoleFallback}
	default:
		return []Role{role}
	}
}

// ConfiguredModels returns all configured role→model entries.
// Used to build model candidate lists for directive resolution.
func (r *Registry) ConfiguredModels() map[Role]ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[Role]ModelConfig, len(r.models))
	for role, cfg := range r.models {
		out[role] = cfg
	}
	return out
}

// SetRoleModelID re-resolves a role to the given "provider/model" ID at
// runtime and resets the role's cached client so the next Client(role) call
// rebuilds against the new endpoint. Returns the resolved config. Cache-safe
// for lightweight/fallback: those roles don't feed the static system-prompt
// cache (built around the main model + toolset).
func (r *Registry) SetRoleModelID(role Role, modelID string) ModelConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg := resolveModelConfig(modelID, r.providers)
	reconcileVllmModel(r.logger, &cfg)
	r.models[role] = cfg
	r.clients[role] = &clientEntry{}
	r.logger.Info("modelrole: role model updated",
		"role", role, "model", logModelAlias(cfg))
	return cfg
}

// ParseModelID splits "provider/model" into provider and model name.
// If no "/" prefix, returns empty provider and the original string.
func ParseModelID(model string) (providerID, modelName string) {
	for i := range len(model) {
		if model[i] == '/' {
			return model[:i], model[i+1:]
		}
	}
	return "", model
}

// logModelAlias returns a short, display-only alias for startup logs.
func logModelAlias(cfg ModelConfig) string {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return strings.TrimSpace(cfg.ProviderID)
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		model = model[idx+1:]
	}
	return model
}

// resolveBaseURL returns the default base URL for a known provider.
func resolveBaseURL(providerID string) string {
	switch providerID {
	case "zai":
		return DefaultZaiBaseURL
	case "localai":
		return DefaultLocalAIBaseURL
	case "vllm":
		return DefaultVllmBaseURL
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "mimo":
		return DefaultMimoBaseURL
	case "mimo-plan":
		return DefaultMimoPlanBaseURL
	case "kimi":
		return DefaultKimiBaseURL
	default:
		return DefaultZaiBaseURL // assume zai for unknown
	}
}

// resolveLocalAIAPIKey reads LOCAL_AI_API_KEY from environment, defaulting to "local"
// for local AI servers that require a bearer token.
// Falls back to legacy SGLANG_API_KEY for backward compatibility.
func resolveLocalAIAPIKey() string {
	if key := os.Getenv("LOCAL_AI_API_KEY"); key != "" {
		return key
	}
	if key := os.Getenv("SGLANG_API_KEY"); key != "" {
		return key
	}
	return "local"
}

// resolveVllmAPIKey reads VLLM_API_KEY from environment, defaulting to "local"
// for local vLLM servers that accept any bearer token.
func resolveVllmAPIKey() string {
	if key := os.Getenv("VLLM_API_KEY"); key != "" {
		return key
	}
	return "local"
}

// resolveAPIKey attempts to resolve an API key for a provider from environment.
func resolveAPIKey(providerID string) string {
	switch providerID {
	case "localai":
		return resolveLocalAIAPIKey()
	case "vllm":
		return resolveVllmAPIKey()
	case "zai":
		return os.Getenv("ZAI_API_KEY")
	case "openrouter":
		return os.Getenv("OPENROUTER_API_KEY")
	case "mimo", "mimo-plan":
		return os.Getenv("XIAOMI_MIMO_API_KEY")
	case "kimi":
		return os.Getenv("KIMI_API_KEY")
	default:
		return ""
	}
}

// DefaultHeaders returns built-in HTTP headers for a provider. The
// coding-subscription providers (Kimi Code, MiMo Token Plan) gate access
// on the client identifier, so they get a coding-agent User-Agent — they
// would otherwise be rejected outright. A `headers` entry in deneb.json
// overrides these per provider. Returns nil for providers with no
// built-in headers. Each call returns a fresh map, safe for the caller
// to mutate (e.g. to merge config overrides).
func DefaultHeaders(providerID string) map[string]string {
	switch providerID {
	case "kimi", "mimo-plan":
		return map[string]string{"User-Agent": codingAgentUserAgent}
	default:
		return nil
	}
}

// ResolveAuthScheme returns the credential scheme for a provider's
// Anthropic Messages requests. The coding-subscription providers (Kimi
// Code, MiMo, MiMo Token Plan) authenticate with OAuth-style Bearer
// tokens; other Anthropic-mode providers (Z.ai) use the default
// x-api-key header. Returns "" to leave the client default.
func ResolveAuthScheme(providerID string) string {
	switch providerID {
	case "kimi", "mimo", "mimo-plan":
		return llm.AuthSchemeBearer
	default:
		return ""
	}
}

// resolveAPIMode returns the LLM client API mode for built-in providers.
// Z.ai, Xiaomi MiMo, and Kimi Code default to the Anthropic Messages API;
// other built-in providers (vllm, localai) speak OpenAI-compatible
// /chat/completions.
func resolveAPIMode(providerID string) string {
	switch providerID {
	case "zai", "zai-subagent", "mimo", "mimo-plan", "kimi":
		return llm.APIModeAnthropic
	default:
		return ""
	}
}
