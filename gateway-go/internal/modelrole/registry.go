// Package modelrole provides a centralized model role registry for the gateway.
//
// Four model roles are defined (main, lightweight, fallback, image), each with
// a provider, model name, base URL, and API type. Subsystems declare which ROLE
// they need (e.g., "lightweight"); the registry resolves the concrete model
// config and provides a cached LLM client.
//
// Fallback chains are automatic:
//
//	Main       → Lightweight → Fallback
//	Lightweight → Fallback
//	Image      → Fallback
//	Fallback   → (none)
package modelrole

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Role identifies a model purpose within the system.
type Role string

const (
	RoleMain        Role = "main"
	RoleLightweight Role = "lightweight"
	RoleFallback    Role = "fallback"
	RoleImage       Role = "image"
)

// ModelConfig holds the provider and endpoint settings for a single model role.
type ModelConfig struct {
	ProviderID string // e.g., "zai", "sglang", "google"
	Model      string // model name sent to the API
	BaseURL    string // API endpoint URL
	APIKey     string // empty for keyless providers (e.g., local sglang)
	APIType    string // "openai" or "anthropic"
}

// clientEntry caches a lazily-initialized LLM client per role.
type clientEntry struct {
	once   sync.Once
	client *llm.Client
}

// Registry holds the four configured model roles and provides resolution,
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

	DefaultZaiBaseURL = "https://api.z.ai/api/coding/paas/v4"
	DefaultZaiModel   = "glm-5-turbo"

	DefaultGoogleBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	DefaultFallbackModel = "gemini-3.1-pro"
	DefaultImageModel    = "gemini-3.1-pro"
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
	mainAPIType := inferAPIType(mainProvider)
	mainAPIKey := resolveAPIKey(mainProvider)

	// Resolve Google API key for fallback/image models.
	googleAPIKey := os.Getenv("GEMINI_API_KEY")

	models := map[Role]ModelConfig{
		RoleMain: {
			ProviderID: mainProvider,
			Model:      mainModelName,
			BaseURL:    mainBaseURL,
			APIType:    mainAPIType,
			APIKey:     mainAPIKey,
		},
		RoleLightweight: {
			ProviderID: "sglang",
			Model:      DefaultSglangModel,
			BaseURL:    DefaultSglangBaseURL,
			APIType:    "openai",
			APIKey:     "", // local, no auth
		},
		RoleFallback: {
			ProviderID: "google",
			Model:      DefaultFallbackModel,
			BaseURL:    DefaultGoogleBaseURL,
			APIType:    "openai",
			APIKey:     googleAPIKey,
		},
		RoleImage: {
			ProviderID: "google",
			Model:      DefaultImageModel,
			BaseURL:    DefaultGoogleBaseURL,
			APIType:    "openai",
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
		"main", fmt.Sprintf("%s/%s", models[RoleMain].ProviderID, models[RoleMain].Model),
		"lightweight", fmt.Sprintf("%s/%s", models[RoleLightweight].ProviderID, models[RoleLightweight].Model),
		"fallback", fmt.Sprintf("%s/%s", models[RoleFallback].ProviderID, models[RoleFallback].Model),
		"image", fmt.Sprintf("%s/%s", models[RoleImage].ProviderID, models[RoleImage].Model),
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

// APIType returns the API type for the given role.
func (r *Registry) APIType(role Role) string {
	return r.Config(role).APIType
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
// "fallback", "image") into the actual full model ID. If the string is already a
// model name (not a role), it is returned unchanged along with ok=false.
// This allows callers to accept either role names or raw model names.
func (r *Registry) ResolveModel(modelOrRole string) (fullModelID string, role Role, ok bool) {
	switch Role(modelOrRole) {
	case RoleMain, RoleLightweight, RoleFallback, RoleImage:
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
	for role, cfg := range r.models {
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
	case RoleImage:
		return []Role{RoleImage, RoleFallback}
	case RoleFallback:
		return []Role{RoleFallback}
	default:
		return []Role{role}
	}
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

// resolveBaseURL returns the default base URL for a known provider.
func resolveBaseURL(providerID string) string {
	switch providerID {
	case "zai":
		return DefaultZaiBaseURL
	case "sglang":
		return DefaultSglangBaseURL
	case "google":
		return DefaultGoogleBaseURL
	case "anthropic":
		return "https://api.anthropic.com"
	default:
		return DefaultZaiBaseURL // assume zai for unknown
	}
}

// inferAPIType guesses the API type from the provider ID.
func inferAPIType(providerID string) string {
	if providerID == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

// resolveAPIKey attempts to resolve an API key for a provider from environment.
func resolveAPIKey(providerID string) string {
	switch providerID {
	case "sglang":
		return "" // local, no auth
	case "google":
		return os.Getenv("GEMINI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	default:
		// For zai and others, keys are resolved through AuthManager at call time.
		return ""
	}
}
