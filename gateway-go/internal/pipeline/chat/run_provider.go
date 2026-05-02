package chat

import (
	"context"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
)

// resolveClient creates an LLM client from provider configs, auth manager,
// provider runtime resolver, or falls back to the pre-configured client.
func resolveClient(deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	// 1. Try provider config from deneb.json.
	if deps.providerConfigs != nil && providerID != "" {
		if cfg, ok := deps.providerConfigs[providerID]; ok {
			baseURL := strings.TrimSpace(provider.ExpandEnvVars(cfg.BaseURL))
			if baseURL == "" {
				baseURL = resolveDefaultBaseURL(providerID)
			}
			apiKey := strings.TrimSpace(provider.ExpandEnvVars(cfg.APIKey))

			// Apply provider runtime auth override (e.g., token exchange).
			if deps.providerRuntime != nil && providerID != "" {
				authResult, err := deps.providerRuntime.PrepareRuntimeAuth(
					context.Background(), providerID,
					provider.RuntimeAuthContext{
						Provider: providerID,
						APIKey:   apiKey,
					},
				)
				if err != nil {
					logger.Warn("provider runtime auth failed", "provider", providerID, "error", err)
				} else if authResult != nil {
					if authResult.APIKey != "" {
						apiKey = authResult.APIKey
					}
					if authResult.BaseURL != "" {
						baseURL = authResult.BaseURL
					}
				}
			}

			if baseURL == "" {
				logger.Warn("provider config missing base URL", "provider", providerID)
			} else {
				opts := []llm.ClientOption{llm.WithLogger(logger)}
				if mode := apiModeFor(providerID, cfg.API); mode != "" {
					opts = append(opts, llm.WithAPIMode(mode))
				}
				client := llm.NewClient(baseURL, apiKey, opts...)
				logger.Info("using provider from config",
					"provider", providerID, "apiMode", apiModeFor(providerID, cfg.API))
				return client
			}
		}
	}

	// 2. Try auth manager.
	if deps.authManager != nil {
		target := providerID
		if target == "" {
			target = "zai" // Default provider: Z.ai Coding Plan (OpenAI-compatible).
		}
		cred := deps.authManager.Resolve(target, "")
		if cred != nil && !cred.IsExpired() && cred.APIKey != "" {
			base := cred.BaseURL
			apiKey := cred.APIKey
			if base == "" {
				base = resolveDefaultBaseURL(target)
			}

			// Apply provider runtime auth override on auth-manager credentials.
			if deps.providerRuntime != nil {
				authResult, err := deps.providerRuntime.PrepareRuntimeAuth(
					context.Background(), target,
					provider.RuntimeAuthContext{
						Provider: target,
						APIKey:   apiKey,
					},
				)
				if err != nil {
					logger.Warn("provider runtime auth failed", "provider", target, "error", err)
				} else if authResult != nil {
					if authResult.APIKey != "" {
						apiKey = authResult.APIKey
					}
					if authResult.BaseURL != "" {
						base = authResult.BaseURL
					}
				}
			}

			opts := []llm.ClientOption{llm.WithLogger(logger)}
			if mode := apiModeFor(target, ""); mode != "" {
				opts = append(opts, llm.WithAPIMode(mode))
			}
			return llm.NewClient(base, apiKey, opts...)
		}
	}

	// 3. Try registry: the modelrole.Registry has cached clients for known
	// provider/role mappings (vllm, google, localai, etc.) with correct base
	// URLs and API keys. This covers model-switch scenarios (e.g., /model
	// vllm/gemma4) where providerConfigs and authManager have no entry.
	if deps.registry != nil && providerID != "" {
		for _, role := range []modelrole.Role{modelrole.RoleMain, modelrole.RoleLightweight, modelrole.RoleFallback} {
			cfg := deps.registry.Config(role)
			if cfg.ProviderID == providerID {
				if client := deps.registry.Client(role); client != nil {
					logger.Info("using provider from registry", "provider", providerID, "role", string(role))
					return client
				}
			}
		}
	}

	// 4. Fall back to pre-configured client.
	return deps.llmClient
}

// Default base URLs for known providers (used when config doesn't specify one).
const (
	// Z.ai Coding Plan Anthropic-compatible endpoint. The gateway speaks the
	// Anthropic Messages API to z.ai so beta features (interleaved thinking,
	// extended thinking, prompt caching) work end-to-end. Operators that
	// explicitly want the OpenAI-compatible coding endpoint can override
	// `baseUrl` and `api` in deneb.json.
	defaultZaiBaseURL = "https://api.z.ai/api/anthropic"
)

// resolveDefaultBaseURL returns the default API base URL for a known provider
// when no explicit base URL is configured.
func resolveDefaultBaseURL(providerID string) string {
	switch providerID {
	case "zai", "zai-subagent":
		return defaultZaiBaseURL
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "localai":
		return modelrole.DefaultLocalAIBaseURL
	case "vllm":
		return modelrole.DefaultVllmBaseURL
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	default:
		return ""
	}
}

// apiModeFor returns the LLM client API mode for a provider. Explicit
// configValue (the `api` field on the provider config) wins; otherwise
// providers known to default to Anthropic-compatible endpoints (z.ai)
// are routed through the Anthropic Messages client. Unknown values fall
// back to OpenAI-compatible (empty string lets the caller skip the option).
func apiModeFor(providerID, configValue string) string {
	switch strings.ToLower(strings.TrimSpace(configValue)) {
	case "anthropic", "anthropic-messages":
		return llm.APIModeAnthropic
	case "openai", "openai-chat", "openai-completions":
		return llm.APIModeOpenAI
	}
	switch providerID {
	case "zai", "zai-subagent":
		return llm.APIModeAnthropic
	}
	return ""
}
