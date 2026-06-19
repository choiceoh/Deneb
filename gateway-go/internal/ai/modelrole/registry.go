// Package modelrole provides a centralized model role registry for the gateway.
//
// Model roles are defined (main, lightweight, tiny, analysis, coding, fallback, ...), each with
// a provider, model name, base URL, and API type. Subsystems declare which ROLE
// they need (e.g., "lightweight"); the registry resolves the concrete model
// config and provides a cached LLM client.
//
// Fallback chains are automatic:
//
//	Main       → Lightweight → Fallback
//	Coding     → Main → Fallback
//	Lightweight → Fallback
//	Fallback   → (none)
package modelrole

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Role identifies a model purpose within the system.
type Role string

const (
	RoleMain        Role = "main"
	RoleTiny        Role = "tiny"        // smallest model: trivial classification/extraction
	RoleLightweight Role = "lightweight" // mid model: bounded summarization
	RoleAnalysis    Role = "analysis"    // highest-quality local model: reasoning-grade tasks
	// RoleCoding is the opt-in model used for code-writing/editing work such as
	// implementer sub-agents and lifecycle-managed skill rewrites. It stays
	// separate from RoleMain so a coding-specialized subscription/model can
	// patch files without taking over the general assistant.
	RoleCoding   Role = "coding"
	RoleFallback Role = "fallback"
	// RoleChatbot is the 챗봇 workspace model (chat: sessions), distinct from
	// RoleMain (업무, client: sessions) so focused general chat can run a
	// different/lighter model. OPT-IN: the role is absent unless
	// agents.chatbotModel is configured; when absent, 챗봇 turns use the main
	// model (see resolveModel in the chat pipeline).
	RoleChatbot Role = "chatbot"
	// RoleVision is the multimodal model used to "see" image inputs. The main
	// model (e.g. DeepSeek-V4-Flash) has no vision tower, so a turn carrying an
	// image routes here instead of being sent to a model that would strip or
	// reject it. OPT-IN: absent unless agents.visionModel is configured; when
	// absent, image turns fall through to the main model exactly as before.
	RoleVision Role = "vision"
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

	// Capability overrides from the provider's deneb.json entry. They are
	// layered over the modelcaps builtin defaults (and vLLM discovery) by
	// CapabilityForModel. Nil pointers / zero ContextWindow mean "no
	// override" — the lower layer's value stands.
	ContextWindow int   // context length in tokens; 0 = no override
	Reasoning     *bool // genuine reasoning-endpoint model
	Vision        *bool // false → image blocks are stripped before sending
	PromptCache   *bool // false → cache_control markers are stripped

	// Sampling overrides layered over the builtin ProfileFor table by
	// ProfileForModel. Nil means "no override".
	Temperature *float64
	TopP        *float64
	TopK        *int

	// Routing overrides the per-model effort-routing policy, layered over
	// router.DefaultProfile() by RoutingProfileForModel. Nil means "no
	// override — the builtin profile stands". This is the per-model knob that
	// lets an operator enable routing for a new dual-mode model, point it at
	// that model's template toggle, or retune the gates without a code change.
	Routing *RoutingOverride
}

// RoutingOverride carries optional per-model effort-router tuning from a
// provider's deneb.json entry. Every field is a pointer: nil leaves the builtin
// (router.DefaultProfile + capability toggle) value in place, so an absent or
// partial block only changes what it names.
type RoutingOverride struct {
	Enabled           *bool   // master switch for this model's routing
	ToggleKwarg       *string // override the chat_template_kwargs off-switch name
	MaxSimpleRunes    *int    // turn-0 query-length gate (primary volume lever)
	StepCeilingTurn   *int    // hard ceiling turn after which thinking always reverts
	ObservationRunes  *int    // single tool-result size that reverts to thinking
	CumulativeRunes   *int    // whole-run tool-output size that reverts to thinking
	HeavyHistoryRunes *int    // assistant-message length that marks context heavy
}

// RegistryOptions configures NewRegistryWithOptions.
type RegistryOptions struct {
	MainModel        string // "provider/model"; empty → local vLLM
	LocalVllmModel   string // served model name for the local vLLM default
	LightweightModel string // override for RoleLightweight; empty → local vLLM
	TinyModel        string // override for RoleTiny; empty → same as lightweight
	AnalysisModel    string // override for RoleAnalysis; empty → same as lightweight
	// CodingModel overrides RoleCoding (code-writing/editing work). Empty → the
	// role is absent and implementer sub-agents fall back to their normal default.
	CodingModel   string // format: "provider/model"
	FallbackModel string // override for RoleFallback; empty → local vLLM
	// ChatbotModel overrides RoleChatbot (챗봇 workspace). Empty → the role is
	// absent and 챗봇 turns fall back to the main model (prior behavior).
	ChatbotModel string
	// VisionModel overrides RoleVision (multimodal/image turns). Empty → the role
	// is absent and image turns use the main model. Format: "provider/model".
	VisionModel string
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
// client caching, fallback chain logic, capability lookup, and model health
// tracking.
//
// Lock hierarchy: mu and health.mu are independent — never hold both at once.
type Registry struct {
	mu        sync.RWMutex
	models    map[Role]ModelConfig
	clients   map[Role]*clientEntry
	providers map[string]ProviderResolved // deneb.json catalog, for runtime role re-resolution
	// vllmWindows caches context lengths (max_model_len) reported by the
	// local vLLM /models discovery, keyed by served model id. Consulted by
	// CapabilityForModel for provider "vllm" only.
	vllmWindows map[string]int
	// vllmProbedAt rate-limits RefreshVllmRole re-discovery (per role).
	vllmProbedAt map[Role]time.Time
	// tunedMaxTokens holds per-model output-token floors written by the
	// background model tuner (tuning.go).
	tunedMaxTokens map[string]int
	// health tracks per-model failure streaks for the circuit breaker
	// (health.go). Guarded by its own mutex, independent of mu.
	health healthState
	logger *slog.Logger
}

// Default constants for known providers.
const (
	DefaultLocalAIBaseURL = "http://127.0.0.1:30000/v1"

	DefaultVllmBaseURL = "http://127.0.0.1:8000/v1"
	DefaultVllmModel   = "gemma4"

	// First-party Anthropic API. The llm client appends /v1/messages in
	// Anthropic mode, so the base URL carries no version segment.
	DefaultAnthropicBaseURL = "https://api.anthropic.com"

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
	// Tiny and Analysis default to the (possibly overridden) lightweight model so
	// an unconfigured deployment behaves exactly as before; deneb.json opts in to
	// a smaller tiny model and a higher-quality analysis model independently.
	models[RoleTiny] = models[RoleLightweight]
	models[RoleAnalysis] = models[RoleLightweight]
	if opts.TinyModel != "" {
		models[RoleTiny] = resolveModelConfig(opts.TinyModel, opts.Providers)
	}
	if opts.AnalysisModel != "" {
		models[RoleAnalysis] = resolveModelConfig(opts.AnalysisModel, opts.Providers)
	}
	if opts.CodingModel != "" {
		models[RoleCoding] = resolveModelConfig(opts.CodingModel, opts.Providers)
	}
	// Chatbot role is OPT-IN: only added to the map when explicitly configured,
	// so an unconfigured deployment leaves 챗봇 turns on the main model. Its
	// presence in the map is what resolveModel keys off to activate the role.
	if opts.ChatbotModel != "" {
		models[RoleChatbot] = resolveModelConfig(opts.ChatbotModel, opts.Providers)
	}
	// Vision role is OPT-IN like chatbot: present only when configured, so an
	// unconfigured deployment leaves image turns on the main model.
	if opts.VisionModel != "" {
		models[RoleVision] = resolveModelConfig(opts.VisionModel, opts.Providers)
	}

	// Auto-discover the actual model name the local vLLM is serving and
	// substitute it in when config drifts. reconcileVllmModel is a no-op for
	// non-vllm roles, so running it across all roles is safe. The discovery
	// payload also carries each model's max_model_len; collect it so
	// CapabilityForModel can clamp context budgets against the real window.
	// Chatbot is included only when present so the loop never inserts a phantom
	// (empty) entry that would make the opt-in role look configured.
	vllmWindows := make(map[string]int)
	probedVllmURLs := make(map[string]bool)
	reconcileRoles := []Role{RoleMain, RoleTiny, RoleLightweight, RoleAnalysis, RoleFallback}
	if _, ok := models[RoleCoding]; ok {
		reconcileRoles = append(reconcileRoles, RoleCoding)
	}
	if _, ok := models[RoleChatbot]; ok {
		reconcileRoles = append(reconcileRoles, RoleChatbot)
	}
	if _, ok := models[RoleVision]; ok {
		reconcileRoles = append(reconcileRoles, RoleVision)
	}
	for _, role := range reconcileRoles {
		cfg := models[role]
		if cfg.ProviderID == "vllm" && cfg.BaseURL != "" {
			probedVllmURLs[cfg.BaseURL] = true
		}
		for _, info := range reconcileVllmModel(logger, &cfg) {
			if info.MaxModelLen > 0 {
				vllmWindows[info.ID] = info.MaxModelLen
			}
		}
		models[role] = cfg
	}
	// Also harvest windows from configured direct-vLLM providers no role routes
	// through: when the main model is fronted by wormhole, the window lives on the
	// still-configured vllm provider, not the proxy. CapabilityForModel applies it
	// by served model id to vLLM-backed fronts (vllm + wormhole).
	harvestVllmWindows(logger, opts.Providers, vllmWindows, probedVllmURLs)

	r := &Registry{
		models:         models,
		clients:        make(map[Role]*clientEntry),
		providers:      opts.Providers,
		vllmWindows:    vllmWindows,
		vllmProbedAt:   make(map[Role]time.Time),
		tunedMaxTokens: make(map[string]int),
		health:         healthState{models: make(map[string]*modelHealth)},
		logger:         logger,
	}

	// Pre-create client entries for lazy initialization.
	for role := range models {
		r.clients[role] = &clientEntry{}
	}

	logger.Info("modelrole: registry initialized",
		"main", logModelAlias(models[RoleMain]),
		"tiny", logModelAlias(models[RoleTiny]),
		"lightweight", logModelAlias(models[RoleLightweight]),
		"analysis", logModelAlias(models[RoleAnalysis]),
		"coding", logModelAlias(models[RoleCoding]),
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

// APIKey returns the API key for the given role (empty for keyless providers).
func (r *Registry) APIKey(role Role) string {
	return r.Config(role).APIKey
}

// VllmBaseURLs returns the deduped base URLs (".../v1") of every role that
// targets the OpenAI-mode vLLM provider, main role first. The observation
// plane scrapes each endpoint's /metrics for engine-level prefix-cache
// counters — some vLLM builds never fill the per-request usage field
// (prompt_tokens_details.cached_tokens), so the engine counters are the only
// reliable cache-hit signal. Non-vLLM deployments return nil.
func (r *Registry) VllmBaseURLs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	seen := make(map[string]bool)
	for _, role := range []Role{RoleMain, RoleAnalysis, RoleCoding, RoleLightweight, RoleTiny, RoleFallback} {
		cfg, ok := r.models[role]
		if !ok || cfg.ProviderID != "vllm" || cfg.BaseURL == "" || seen[cfg.BaseURL] {
			continue
		}
		// /metrics lives on vLLM's OpenAI server; an anthropic-mode endpoint
		// under the vllm provider id would be some other proxy — skip it.
		if cfg.APIMode == llm.APIModeAnthropic {
			continue
		}
		seen[cfg.BaseURL] = true
		out = append(out, cfg.BaseURL)
	}
	return out
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
	case "anthropic", "zai", "localai", "vllm", "openrouter", "mimo", "mimo-plan", "kimi":
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
	case RoleMain, RoleTiny, RoleLightweight, RoleAnalysis, RoleFallback:
		role = Role(modelOrRole)
		return r.FullModelID(role), role, true
	case RoleCoding:
		role = Role(modelOrRole)
		if id := r.FullModelID(role); id != "" {
			return id, role, true
		}
	}
	return modelOrRole, "", false
}

// RoleForModel returns the role that matches the given full model ID (e.g., "google/gemini-3.1-pro").
// Returns ("", false) if no role matches.
func (r *Registry) RoleForModel(fullModelID string) (Role, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Lightweight precedes tiny/analysis so a deployment that leaves them at the
	// lightweight default maps that shared model back to the lightweight role
	// (preserving prior behavior); an explicitly configured tiny/analysis model
	// still matches its own role.
	for _, role := range []Role{RoleMain, RoleCoding, RoleLightweight, RoleTiny, RoleAnalysis, RoleFallback, RoleChatbot, RoleVision} {
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
	case RoleTiny:
		return []Role{RoleTiny, RoleLightweight, RoleFallback}
	case RoleLightweight:
		return []Role{RoleLightweight, RoleFallback}
	case RoleAnalysis:
		return []Role{RoleAnalysis, RoleLightweight, RoleFallback}
	case RoleCoding:
		// Code edits are quality-sensitive and tool-heavy. If the dedicated
		// coding role fails, degrade to the general main model before the shared
		// fallback instead of a smaller summarization role.
		return []Role{RoleCoding, RoleMain, RoleFallback}
	case RoleChatbot:
		// On chatbot-model failure, degrade to the main (업무) model, then the
		// shared fallback — so a bad chatbot model never leaves 챗봇 dead.
		return []Role{RoleChatbot, RoleMain, RoleFallback}
	case RoleVision:
		// On vision-model failure, degrade straight to the shared fallback —
		// NOT the main model, which has no vision tower and would reject the
		// image. If the fallback also can't see, the request errors clearly.
		return []Role{RoleVision, RoleFallback}
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
	for _, info := range reconcileVllmModel(r.logger, &cfg) {
		if info.MaxModelLen > 0 {
			r.vllmWindows[info.ID] = info.MaxModelLen
		}
	}
	r.models[role] = cfg
	r.clients[role] = &clientEntry{}
	r.logger.Info("modelrole: role model updated",
		"role", role, "model", logModelAlias(cfg))
	return cfg
}

// ClearRole removes a role's explicit model so it reverts to its default
// resolution. Used when the model a role pointed at is deleted. The always-on
// roles (main/lightweight/fallback/tiny/analysis) are reset via SetRoleModelID
// instead; this is for opt-in roles like RoleChatbot/RoleCoding that should disappear —
// and fall back to the main model — when left unconfigured.
func (r *Registry) ClearRole(role Role) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.models, role)
	delete(r.clients, role)
	r.logger.Info("modelrole: role cleared", "role", role)
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
	case "anthropic":
		return DefaultAnthropicBaseURL
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
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
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
// The first-party Anthropic API, Z.ai, Xiaomi MiMo, and Kimi Code default
// to the Anthropic Messages API; other built-in providers (vllm, localai)
// speak OpenAI-compatible /chat/completions.
func resolveAPIMode(providerID string) string {
	switch providerID {
	case "anthropic", "zai", "zai-subagent", "mimo", "mimo-plan", "kimi":
		return llm.APIModeAnthropic
	default:
		return ""
	}
}
