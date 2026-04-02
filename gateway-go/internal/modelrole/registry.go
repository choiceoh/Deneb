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

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Role identifies a model purpose within the system.
type Role string

const (
	RoleMain        Role = "main"
	RoleLightweight Role = "lightweight"
	RolePilot       Role = "pilot"
	RoleFallback    Role = "fallback"
)

// ModelConfig holds the provider and endpoint settings for a single model role.
type ModelConfig struct {
	ProviderID string // e.g., "zai", "sglang", "google"
	Model      string // model name sent to the API
	BaseURL    string // API endpoint URL
	APIKey     string // empty for keyless providers (e.g., local sglang)
}

// clientEntry caches a lazily-initialized LLM client per role.
type clientEntry struct {
	once   sync.Once
	client *llm.Client
}

// Registry holds the configured model roles and provides resolution,
// client caching, and fallback chain logic.
type Registry struct {
	mu      sync.RWMutex
	models  map[Role]ModelConfig
	clients map[Role]*clientEntry
	logger  *slog.Logger
}

// Default constants for known providers.
const (
	DefaultSglangBaseURL = "http://127.0.0.1:30000/v1"
	DefaultSglangModel   = "Qwen/Qwen3.5-35B-A3B"

	DefaultVllmBaseURL = "http://127.0.0.1:8000/v1"

	DefaultZaiBaseURL = "https://api.z.ai/api/coding/paas/v4"
	DefaultZaiModel   = "glm-5-turbo"

	DefaultGoogleBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	DefaultPilotModel    = "gemini-3-flash-preview"
	DefaultFallbackModel = "gemini-3.1-pro"
)

// NewRegistry creates a registry with hardcoded defaults.
// mainModel is the resolved default model from deneb.json (e.g., "zai/some-model").
// If mainModel is empty, a sensible default is used.
func NewRegistry(logger *slog.Logger, mainModel string) *Registry {
	if logger == nil {
		logger = slog.Default()
	}

	// Fall back to default Z.AI model when no model is configured.
	if mainModel == "" {
		mainModel = "zai/" + DefaultZaiModel
	}

	// Parse main model provider/name.
	mainProvider, mainModelName := parseModelID(mainModel)
	mainBaseURL := resolveBaseURL(mainProvider)
	mainAPIKey := resolveAPIKey(mainProvider)

	// Resolve Google API key for fallback/pilot models.
	googleAPIKey := os.Getenv("GEMINI_API_KEY")

	models := map[Role]ModelConfig{
		RoleMain: {
			ProviderID: mainProvider,
			Model:      mainModelName,
			BaseURL:    mainBaseURL,
			APIKey:     mainAPIKey,
		},
		RoleLightweight: {
			ProviderID: "sglang",
			Model:      DefaultSglangModel,
			BaseURL:    DefaultSglangBaseURL,
			APIKey:     resolveSglangAPIKey(),
		},
		RolePilot: {
			ProviderID: "google",
			Model:      DefaultPilotModel,
			BaseURL:    DefaultGoogleBaseURL,
			APIKey:     googleAPIKey,
		},
		RoleFallback: {
			ProviderID: "google",
			Model:      DefaultFallbackModel,
			BaseURL:    DefaultGoogleBaseURL,
			APIKey:     googleAPIKey,
		},
	}

	r := &Registry{
		models:  models,
		clients: make(map[Role]*clientEntry),
		logger:  logger,
	}

	// Pre-create client entries for lazy initialization.
	for role := range models {
		r.clients[role] = &clientEntry{}
	}

	logger.Info("modelrole: registry initialized",
		"main", logModelAlias(models[RoleMain]),
		"lightweight", logModelAlias(models[RoleLightweight]),
		"pilot", logModelAlias(models[RolePilot]),
		"fallback", logModelAlias(models[RoleFallback]),
	)

	return r
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
		entry.client = llm.NewClient(cfg.BaseURL, cfg.APIKey, llm.WithLogger(r.logger))
	})
	return entry.client
}

// ResolveModel resolves a model string that may be a role name ("main", "lightweight",
// "pilot", "fallback") into the actual full model ID. If the string is already a
// model name (not a role), it is returned unchanged along with ok=false.
// This allows callers to accept either role names or raw model names.
func (r *Registry) ResolveModel(modelOrRole string) (fullModelID string, role Role, ok bool) {
	switch Role(modelOrRole) {
	case RoleMain, RoleLightweight, RolePilot, RoleFallback:
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
	for _, role := range []Role{RoleMain, RoleLightweight, RolePilot, RoleFallback} {
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
	case RolePilot:
		return []Role{RolePilot, RoleFallback}
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

// parseModelID splits "provider/model" into provider and model name.
// If no "/" prefix, returns empty provider and the original string.
func parseModelID(model string) (providerID, modelName string) {
	for i := 0; i < len(model); i++ {
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
	case "sglang":
		return DefaultSglangBaseURL
	case "vllm":
		return DefaultVllmBaseURL
	case "google":
		return DefaultGoogleBaseURL
	default:
		return DefaultZaiBaseURL // assume zai for unknown
	}
}


// resolveSglangAPIKey reads SGLANG_API_KEY from environment, defaulting to "local"
// for local sglang servers that require a bearer token.
func resolveSglangAPIKey() string {
	if key := os.Getenv("SGLANG_API_KEY"); key != "" {
		return key
	}
	return "local"
}

// resolveAPIKey attempts to resolve an API key for a provider from environment.
func resolveAPIKey(providerID string) string {
	switch providerID {
	case "sglang":
		return resolveSglangAPIKey()
	case "google":
		return os.Getenv("GEMINI_API_KEY")
	case "zai":
		return os.Getenv("ZAI_API_KEY")
	default:
		return ""
	}
}
