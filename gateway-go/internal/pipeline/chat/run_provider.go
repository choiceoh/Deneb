package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
)

// runtimeAuthTimeout bounds the provider runtime auth call (token exchange —
// a network round-trip) on the client-resolve path. Derived from the request
// ctx so a canceled turn does not leave an orphaned exchange in flight.
const runtimeAuthTimeout = 15 * time.Second

// resolveClient creates an LLM client from provider configs, auth manager,
// provider runtime resolver, or falls back to the pre-configured client.
// ctx is the turn's request context; runtime auth (token exchange) is bounded
// by runtimeAuthTimeout under it.
func resolveClient(ctx context.Context, deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	if client := configuredProviderClient(ctx, deps, providerID, logger); client != nil {
		return client
	}
	if client := managedProviderClient(ctx, deps, providerID, logger); client != nil {
		return client
	}
	if client := registryProviderClient(deps, providerID, logger); client != nil {
		return client
	}
	return deps.llmClient
}

// configuredProviderClient resolves the highest-priority deneb.json provider
// entry. A configured entry with no usable endpoint deliberately returns nil so
// managed credentials and registry clients retain their historical fallback.
func configuredProviderClient(ctx context.Context, deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	if deps.providerConfigs == nil || providerID == "" {
		return nil
	}
	cfg, ok := deps.providerConfigs[providerID]
	if !ok {
		return nil
	}

	credentials := providerClientCredentials{
		baseURL: strings.TrimSpace(provider.ExpandEnvVars(cfg.BaseURL)),
		apiKey:  resolveProviderAPIKey(providerID, cfg, logger),
	}
	if credentials.baseURL == "" {
		credentials.baseURL = resolveDefaultBaseURL(providerID)
	}
	credentials = prepareRuntimeProviderCredentials(ctx, deps.providerRuntime, providerID, credentials, logger)
	if credentials.baseURL == "" {
		logger.Warn("provider config missing base URL", "provider", providerID)
		return nil
	}

	mode := apiModeFor(providerID, cfg.API)
	client := newResolvedProviderClient(providerID, credentials, mode, cfg.Headers, logger)
	logger.Info("using provider from config", "provider", providerID, "apiMode", mode)
	return client
}

// managedProviderClient resolves credentials maintained by AuthManager. An
// empty provider ID keeps the established Z.ai default before runtime auth is
// applied.
func managedProviderClient(ctx context.Context, deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	if deps.authManager == nil {
		return nil
	}
	target := providerID
	if target == "" {
		target = "zai"
	}
	credential := deps.authManager.Resolve(target, "")
	if credential == nil || credential.IsExpired() || credential.APIKey == "" {
		return nil
	}

	credentials := providerClientCredentials{baseURL: credential.BaseURL, apiKey: credential.APIKey}
	if credentials.baseURL == "" {
		credentials.baseURL = resolveDefaultBaseURL(target)
	}
	credentials = prepareRuntimeProviderCredentials(ctx, deps.providerRuntime, target, credentials, logger)
	return newResolvedProviderClient(target, credentials, apiModeFor(target, ""), nil, logger)
}

type providerClientCredentials struct {
	baseURL string
	apiKey  string
}

// prepareRuntimeProviderCredentials applies an optional token exchange without
// changing source priority. Errors remain warnings and leave the original
// credentials intact, matching the previous best-effort behavior.
func prepareRuntimeProviderCredentials(
	ctx context.Context,
	runtime *provider.ProviderRuntimeResolver,
	providerID string,
	credentials providerClientCredentials,
	logger *slog.Logger,
) providerClientCredentials {
	if runtime == nil {
		return credentials
	}
	authCtx, authCancel := context.WithTimeout(ctx, runtimeAuthTimeout)
	authResult, err := runtime.PrepareRuntimeAuth(authCtx, providerID, provider.RuntimeAuthContext{
		Provider: providerID,
		APIKey:   credentials.apiKey,
	})
	authCancel()
	if err != nil {
		logger.Warn("provider runtime auth failed", "provider", providerID, "error", err)
		return credentials
	}
	if authResult == nil {
		return credentials
	}
	if authResult.APIKey != "" {
		credentials.apiKey = authResult.APIKey
	}
	if authResult.BaseURL != "" {
		credentials.baseURL = authResult.BaseURL
	}
	return credentials
}

func newResolvedProviderClient(
	providerID string,
	credentials providerClientCredentials,
	apiMode string,
	configuredHeaders map[string]string,
	logger *slog.Logger,
) *llm.Client {
	opts := []llm.ClientOption{llm.WithLogger(logger)}
	if apiMode != "" {
		opts = append(opts, llm.WithAPIMode(apiMode))
	}
	if scheme := modelrole.ResolveAuthScheme(providerID); scheme != "" {
		opts = append(opts, llm.WithAuthScheme(scheme))
	}
	if headers := resolvedProviderHeaders(providerID, configuredHeaders); len(headers) > 0 {
		opts = append(opts, llm.WithHeaders(headers))
	}
	return llm.NewClient(credentials.baseURL, credentials.apiKey, opts...)
}

// resolvedProviderHeaders layers explicit configuration over fresh built-in
// headers so caller-owned maps and modelrole defaults are never mutated.
func resolvedProviderHeaders(providerID string, configured map[string]string) map[string]string {
	headers := modelrole.DefaultHeaders(providerID)
	for key, value := range configured {
		if headers == nil {
			headers = make(map[string]string, len(configured))
		}
		headers[key] = value
	}
	return headers
}

// registryProviderClient selects cached role clients in the established
// main→lightweight→fallback order, then asks the registry for an on-demand
// built-in provider client.
func registryProviderClient(deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	if deps.registry == nil || providerID == "" {
		return nil
	}
	for _, role := range []modelrole.Role{modelrole.RoleMain, modelrole.RoleLightweight, modelrole.RoleFallback} {
		if deps.registry.Config(role).ProviderID != providerID {
			continue
		}
		if client := deps.registry.Client(role); client != nil {
			logger.Info("using provider from registry", "provider", providerID, "role", string(role))
			return client
		}
	}
	if client := deps.registry.ClientForProvider(providerID); client != nil {
		logger.Info("using built-in provider", "provider", providerID)
		return client
	}
	return nil
}

func resolveProviderAPIKey(providerID string, cfg ProviderConfig, logger *slog.Logger) string {
	if strings.TrimSpace(cfg.APIKeyRef) != "" {
		logger.Warn("provider apiKeyRef ignored: 1Password secret refs are no longer supported; use a plain key or env var", "provider", providerID)
	}
	return strings.TrimSpace(provider.ExpandEnvVars(cfg.APIKey))
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
	case "anthropic":
		return modelrole.DefaultAnthropicBaseURL
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
	case "mimo":
		return modelrole.DefaultMimoBaseURL
	case "mimo-plan":
		return modelrole.DefaultMimoPlanBaseURL
	case "kimi":
		return modelrole.DefaultKimiBaseURL
	default:
		return ""
	}
}

// apiModeFor returns the LLM client API mode for a provider. Explicit
// configValue (the `api` field on the provider config) wins; otherwise
// providers known to default to Anthropic-compatible endpoints (the
// first-party Anthropic API, z.ai, Xiaomi MiMo, Kimi Code) are routed
// through the Anthropic Messages client. Unknown values fall back to
// OpenAI-compatible (empty string lets the caller skip the option).
func apiModeFor(providerID, configValue string) string {
	switch strings.ToLower(strings.TrimSpace(configValue)) {
	case "anthropic", "anthropic-messages":
		return llm.APIModeAnthropic
	case "openai", "openai-chat", "openai-completions":
		return llm.APIModeOpenAI
	}
	switch providerID {
	case "anthropic", "zai", "zai-subagent", "mimo", "mimo-plan", "kimi":
		return llm.APIModeAnthropic
	}
	return ""
}

// resolveAPIMode resolves the wire protocol (openai/anthropic) for the given
// providerID, factoring in any explicit `api` override in deneb.json's
// provider config. Falls back to the provider default (zai → anthropic) when
// no config entry exists.
func resolveAPIMode(deps runDeps, providerID string) string {
	if deps.providerConfigs != nil {
		if cfg, ok := deps.providerConfigs[providerID]; ok {
			return apiModeFor(providerID, cfg.API)
		}
	}
	return apiModeFor(providerID, "")
}

// isCacheIncompatibleProvider reports whether a provider speaks the Anthropic
// Messages wire but REJECTS cache_control markers with an HTTP 400 (not merely
// ignores them); the markers Deneb attaches (2 system + 2 trailing) must be
// stripped for such providers. The builtin list (Kimi and its aliases) lives in
// modelcaps; a `promptCache` boolean on the provider's deneb.json entry
// overrides it in either direction — see modelCapability in run_capability.go,
// which is what the run path consults.
func isCacheIncompatibleProvider(providerID string) bool {
	return leafbind.RejectsCacheControl(providerID)
}
