package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

const (
	// generatedTokenBytes is the number of random bytes for auto-generated tokens (24 bytes = 48 hex chars).
	generatedTokenBytes = 24
	// maxMediaTTLHours is the maximum media retention window (7 days).
	maxMediaTTLHours = 24 * 7
)

var ErrInvalidCustomModel = errors.New("invalid custom model")

// BootstrapResult is the result of the gateway config bootstrap sequence.
type BootstrapResult struct {
	Config                  DenebConfig
	Snapshot                *ConfigSnapshot
	Auth                    ResolvedGatewayAuth
	GeneratedToken          string
	PersistedGeneratedToken bool
}

// ResolvedGatewayAuth holds the fully resolved gateway authentication state.
type ResolvedGatewayAuth struct {
	Mode     string `json:"mode"` // "none" | "token" | "password" | "trusted-proxy"
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`

	// TrustedProxy holds the resolved trusted-proxy config (when mode=trusted-proxy).
	TrustedProxy *GatewayTrustedProxyConfig `json:"trustedProxy,omitempty"`

	// AllowTailscale indicates Tailscale identity headers are allowed.
	AllowTailscale bool `json:"allowTailscale,omitempty"`
}

// HasSharedSecret returns true if the auth has a non-empty token or password.
func (a *ResolvedGatewayAuth) HasSharedSecret() bool {
	switch a.Mode {
	case AuthModeToken:
		return strings.TrimSpace(a.Token) != ""
	case AuthModePassword:
		return strings.TrimSpace(a.Password) != ""
	default:
		return false
	}
}

// BootstrapGatewayConfig performs the full gateway startup config bootstrap:
//  1. Load and validate the config snapshot.
//  2. Resolve auth (auto-generate token if needed).
//  3. Resolve environment-based secret overrides.
//  4. Optionally persist generated token to config file.
func BootstrapGatewayConfig(opts BootstrapOptions) (*BootstrapResult, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Step 1: Load config.
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = ResolveConfigPath()
	}

	snap, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("config load failed: %w", err)
	}

	// Step 2: Validate snapshot.
	if !snap.Valid {
		issueLines := make([]string, 0, len(snap.Issues))
		for _, issue := range snap.Issues {
			issueLines = append(issueLines, issue.String())
		}
		return nil, fmt.Errorf("invalid config at %s:\n%s\nRun \"deneb doctor\" to repair",
			snap.Path, strings.Join(issueLines, "\n"))
	}

	cfg := snap.Config

	// Step 3: Apply auth overrides from CLI/env.
	if opts.AuthOverride != nil {
		cfg.Gateway.Auth = mergeAuthConfig(cfg.Gateway.Auth, opts.AuthOverride)
	}
	if opts.TailscaleOverride != nil {
		cfg.Gateway.Tailscale = mergeTailscaleConfig(cfg.Gateway.Tailscale, opts.TailscaleOverride)
	}

	// Step 4: Resolve auth (env fallback + auto-generate).
	resolved, generatedToken, err := resolveStartupAuth(&cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("auth bootstrap failed: %w", err)
	}

	// Step 5: Persist generated token if applicable.
	persisted := false
	if generatedToken != "" && opts.Persist && opts.AuthOverride == nil {
		if err := persistGeneratedToken(snap.Path, generatedToken, logger); err != nil {
			logger.Warn("failed to persist generated gateway token", "error", err)
		} else {
			persisted = true
			logger.Info("auto-generated gateway auth token persisted to config")
		}
	}

	// Step 6: Validate hooks token is not the same as gateway auth token.
	if cfg.Hooks != nil && cfg.Hooks.Token != "" && resolved.Token != "" {
		if cfg.Hooks.Token == resolved.Token {
			logger.Warn("hooks.token should differ from gateway.auth.token for security isolation")
		}
	}

	// Step 7: Propagate the configured IANA zone to pkg/dentime so subsequent
	// dentime.Now() calls pick it up. DENEB_TIMEZONE env var still overrides.
	dentime.SetConfigTimezone(cfg.Timezone)

	return &BootstrapResult{
		Config:                  cfg,
		Snapshot:                snap,
		Auth:                    *resolved,
		GeneratedToken:          generatedToken,
		PersistedGeneratedToken: persisted,
	}, nil
}

// BootstrapOptions configures the bootstrap process.
type BootstrapOptions struct {
	ConfigPath        string
	AuthOverride      *GatewayAuthConfig
	TailscaleOverride *GatewayTailscaleConfig
	Persist           bool // Persist generated token to config file.
	Logger            *slog.Logger
}

// resolveStartupAuth resolves the gateway auth mode, token/password from config and env.
// If mode=token and no token is configured, generates a random token.
func resolveStartupAuth(cfg *DenebConfig, logger *slog.Logger) (*ResolvedGatewayAuth, string, error) {
	authCfg := cfg.Gateway.Auth
	mode := authCfg.Mode
	if mode == "" {
		mode = AuthModeToken
	}

	resolved := &ResolvedGatewayAuth{
		Mode: mode,
	}

	var generatedToken string

	switch mode {
	case AuthModeNone:
		// No auth required.

	case AuthModeToken:
		token := resolveSecretValue(authCfg.Token, "DENEB_GATEWAY_TOKEN")
		if token == "" {
			// Auto-generate a random token.
			generated, err := generateRandomToken()
			if err != nil {
				return nil, "", fmt.Errorf("failed to generate gateway token: %w", err)
			}
			token = generated
			generatedToken = generated
			logger.Info("auto-generated gateway auth token (no token configured)")
		}
		resolved.Token = token

	case AuthModePassword:
		password := resolveSecretValue(authCfg.Password, "DENEB_GATEWAY_PASSWORD")
		if password == "" {
			return nil, "", fmt.Errorf("gateway auth mode=password requires a password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)")
		}
		resolved.Password = password

	case AuthModeTrustedProxy:
		if authCfg.TrustedProxy == nil || authCfg.TrustedProxy.UserHeader == "" {
			return nil, "", fmt.Errorf("gateway auth mode=trusted-proxy requires gateway.auth.trustedProxy.userHeader")
		}
		resolved.TrustedProxy = authCfg.TrustedProxy
	}

	// Resolve allowTailscale.
	if authCfg.AllowTailscale != nil {
		resolved.AllowTailscale = *authCfg.AllowTailscale
	}

	return resolved, generatedToken, nil
}

// resolveSecretValue resolves a secret from config value, then env var fallbacks.
func resolveSecretValue(configValue string, envKeys ...string) string {
	if strings.TrimSpace(configValue) != "" {
		return configValue
	}
	for _, key := range envKeys {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return ""
}

// generateRandomToken generates a cryptographically random hex token.
func generateRandomToken() (string, error) {
	b := make([]byte, generatedTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// persistGeneratedToken writes the generated token into the config file.
// If the file doesn't exist, creates it with the token.
func persistGeneratedToken(configPath, token string, _ *slog.Logger) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Read existing config or start with empty object.
	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	// Set gateway.auth.token.
	gw, ok := raw["gateway"].(map[string]any)
	if !ok {
		gw = make(map[string]any)
		raw["gateway"] = gw
	}
	auth, ok := gw["auth"].(map[string]any)
	if !ok {
		auth = make(map[string]any)
		gw["auth"] = auth
	}
	auth["token"] = token

	// Update meta.
	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		raw["meta"] = meta
	}
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// PersistDefaultModel writes the given model ID into agents.defaultModel
// in the config file, preserving all other fields.
func PersistDefaultModel(configPath, model string, logger *slog.Logger) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	// Set agents.defaultModel.
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents["defaultModel"] = model

	// Update meta.
	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		raw["meta"] = meta
	}
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	logger.Info("persisted default model", "model", model, "path", configPath)
	return nil
}

// PersistRoleModel writes a model ID into the agents config field for the
// given modelrole role, preserving all other fields:
//
//	main        → agents.defaultModel
//	lightweight → agents.lightweightModel
//	fallback    → agents.fallbackModel
//
// Mirrors PersistDefaultModel; used by the miniapp per-role model picker.
func PersistRoleModel(configPath, role, model string, logger *slog.Logger) error {
	var field string
	switch role {
	case "main", "":
		field = "defaultModel"
	case "lightweight":
		field = "lightweightModel"
	case "fallback":
		field = "fallbackModel"
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents[field] = model

	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		raw["meta"] = meta
	}
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	logger.Info("persisted role model", "role", role, "field", field, "model", model, "path", configPath)
	return nil
}

// PersistedCustomModel describes the provider/model entry written to config.
type PersistedCustomModel struct {
	ProviderID  string
	ModelID     string
	FullModelID string
	BaseURL     string
	Added       bool
}

// PersistCustomProviderModel writes an OpenAI-compatible endpoint + model into
// models.providers, preserving all other config fields. The newest direct model
// is kept first so it remains visible even when provider lists are capped.
func PersistCustomProviderModel(configPath, endpoint, model string, logger *slog.Logger) (PersistedCustomModel, error) {
	baseURL, err := normalizeCustomModelEndpoint(endpoint)
	if err != nil {
		return PersistedCustomModel{}, err
	}
	modelID, err := normalizeCustomModelID(model)
	if err != nil {
		return PersistedCustomModel{}, err
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PersistedCustomModel{}, fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return PersistedCustomModel{}, fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return PersistedCustomModel{}, fmt.Errorf("parsing config: %w", err)
		}
	}

	models := ensureObject(raw, "models")
	providers := ensureObject(models, "providers")

	providerID, providerConfig := providerForCustomEndpoint(providers, baseURL)
	if providerConfig == nil {
		providerID = nextCustomProviderID(providers)
		providerConfig = map[string]any{
			"baseUrl": baseURL,
			"api":     "openai",
		}
		providers[providerID] = providerConfig
	}
	added := upsertCustomModel(providerConfig, modelID)

	meta := ensureObject(raw, "meta")
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return PersistedCustomModel{}, fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return PersistedCustomModel{}, fmt.Errorf("writing config: %w", err)
	}

	result := PersistedCustomModel{
		ProviderID:  providerID,
		ModelID:     modelID,
		FullModelID: providerID + "/" + modelID,
		BaseURL:     baseURL,
		Added:       added,
	}
	if logger != nil {
		logger.Info("persisted custom model", "provider", providerID, "model", modelID, "path", configPath)
	}
	return result, nil
}

// DeletedCustomModel reports the outcome of removing a custom model entry.
type DeletedCustomModel struct {
	ProviderID      string   // provider the model belonged to
	ModelID         string   // model name removed
	FullModelID     string   // provider/model
	Removed         bool     // a matching model entry was found and removed
	ProviderDropped bool     // provider had no models left and was deleted
	ClearedRoles    []string // agents role fields cleared because they pointed here
}

// DeleteCustomProviderModel removes a user-added model from models.providers,
// preserving every other config field. Only custom providers (custom, custom-N)
// are user-managed and therefore deletable; built-in/role model IDs return
// ErrInvalidCustomModel. When the provider's model list becomes empty the whole
// provider entry is dropped. Any agents.{default,lightweight,fallback}Model that
// referenced the deleted model is cleared so the role falls back to its default
// on the next registry build (the live registry is reset separately by the
// caller). This is the inverse of PersistCustomProviderModel.
func DeleteCustomProviderModel(configPath, fullModelID string, logger *slog.Logger) (DeletedCustomModel, error) {
	providerID, modelID, err := splitCustomModelID(fullModelID)
	if err != nil {
		return DeletedCustomModel{}, err
	}

	result := DeletedCustomModel{
		ProviderID:  providerID,
		ModelID:     modelID,
		FullModelID: providerID + "/" + modelID,
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil // nothing persisted yet — idempotent no-op
		}
		return DeletedCustomModel{}, fmt.Errorf("reading config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return DeletedCustomModel{}, fmt.Errorf("parsing config: %w", err)
	}

	// Remove the model from its provider, dropping the provider when empty.
	if models, ok := raw["models"].(map[string]any); ok {
		if providers, ok := models["providers"].(map[string]any); ok {
			if providerConfig, ok := providers[providerID].(map[string]any); ok {
				if removeCustomModel(providerConfig, modelID) {
					result.Removed = true
					if customModelCount(providerConfig) == 0 {
						delete(providers, providerID)
						result.ProviderDropped = true
					}
				}
			}
		}
	}

	// Clear any role binding that pointed at the deleted model so the role
	// resolves to its default again instead of a now-missing model.
	result.ClearedRoles = clearRolesReferencingModel(raw, result.FullModelID)

	if !result.Removed && len(result.ClearedRoles) == 0 {
		return result, nil // nothing changed — skip the write
	}

	meta := ensureObject(raw, "meta")
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return DeletedCustomModel{}, fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return DeletedCustomModel{}, fmt.Errorf("writing config: %w", err)
	}

	if logger != nil {
		logger.Info("deleted custom model",
			"provider", providerID, "model", modelID,
			"providerDropped", result.ProviderDropped,
			"clearedRoles", result.ClearedRoles, "path", configPath)
	}
	return result, nil
}

func normalizeCustomModelEndpoint(endpoint string) (string, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return "", fmt.Errorf("%w: endpoint is required", ErrInvalidCustomModel)
	}
	if !strings.Contains(raw, "://") {
		return "", fmt.Errorf("%w: endpoint must include http:// or https://", ErrInvalidCustomModel)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: endpoint URL is invalid", ErrInvalidCustomModel)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("%w: endpoint must use http or https", ErrInvalidCustomModel)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%w: endpoint host is required", ErrInvalidCustomModel)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: endpoint must not include query or fragment", ErrInvalidCustomModel)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func normalizeCustomModelID(model string) (string, error) {
	id := strings.TrimSpace(model)
	if id == "" {
		return "", fmt.Errorf("%w: model name is required", ErrInvalidCustomModel)
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
		return "", fmt.Errorf("%w: model name must not start or end with /", ErrInvalidCustomModel)
	}
	if strings.ContainsFunc(id, unicode.IsSpace) {
		return "", fmt.Errorf("%w: model name must not contain whitespace", ErrInvalidCustomModel)
	}
	return id, nil
}

func ensureObject(parent map[string]any, key string) map[string]any {
	if obj, ok := parent[key].(map[string]any); ok {
		return obj
	}
	obj := make(map[string]any)
	parent[key] = obj
	return obj
}

func providerForCustomEndpoint(providers map[string]any, baseURL string) (string, map[string]any) {
	keys := make([]string, 0, len(providers))
	for id := range providers {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		rawProvider := providers[id]
		providerConfig, ok := rawProvider.(map[string]any)
		if !ok {
			continue
		}
		existingBase, ok := providerConfig["baseUrl"].(string)
		if !ok {
			continue
		}
		normalized, err := normalizeCustomModelEndpoint(existingBase)
		if err != nil {
			continue
		}
		if normalized == baseURL {
			return id, providerConfig
		}
	}
	return "", nil
}

func nextCustomProviderID(providers map[string]any) string {
	for i := 1; ; i++ {
		id := "custom"
		if i > 1 {
			id = fmt.Sprintf("custom-%d", i)
		}
		if _, exists := providers[id]; !exists {
			return id
		}
	}
}

func upsertCustomModel(providerConfig map[string]any, modelID string) bool {
	var existing []any
	if arr, ok := providerConfig["models"].([]any); ok {
		existing = arr
	}

	added := true
	front := any(map[string]any{"id": modelID})
	next := []any{}
	for _, item := range existing {
		if existingID := customModelID(item); existingID == modelID {
			added = false
			front = item
			continue
		}
		next = append(next, item)
	}
	next = append([]any{front}, next...)
	providerConfig["models"] = next
	return added
}

func customModelID(item any) string {
	switch v := item.(type) {
	case map[string]any:
		if id, ok := v["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

// splitCustomModelID parses "provider/model" and verifies the provider is a
// user-managed custom provider. Built-in/role model IDs are rejected so the
// delete path can never remove a model the user did not add by hand.
func splitCustomModelID(fullModelID string) (providerID, modelID string, err error) {
	full := strings.TrimSpace(fullModelID)
	if full == "" {
		return "", "", fmt.Errorf("%w: model id is required", ErrInvalidCustomModel)
	}
	slash := strings.IndexByte(full, '/')
	if slash <= 0 || slash >= len(full)-1 {
		return "", "", fmt.Errorf("%w: model id must be provider/model", ErrInvalidCustomModel)
	}
	providerID, modelID = full[:slash], full[slash+1:]
	if !IsCustomProviderID(providerID) {
		return "", "", fmt.Errorf("%w: only directly-added custom models can be deleted", ErrInvalidCustomModel)
	}
	return providerID, modelID, nil
}

// IsCustomProviderID reports whether a provider key is one the Mini App's
// "직접 추가" flow generates: "custom" or "custom-<n>" with a numeric suffix
// (see nextCustomProviderID). A manually named provider such as "custom-prod"
// or "custom-openai" is NOT treated as Mini App-managed, so the picker neither
// shows a delete button for it nor lets delete_custom remove its models.
func IsCustomProviderID(id string) bool {
	if id == "custom" {
		return true
	}
	rest, ok := strings.CutPrefix(id, "custom-")
	if !ok || rest == "" {
		return false
	}
	_, err := strconv.Atoi(rest)
	return err == nil
}

// removeCustomModel drops modelID from providerConfig["models"], reporting
// whether an entry was actually removed.
func removeCustomModel(providerConfig map[string]any, modelID string) bool {
	existing, ok := providerConfig["models"].([]any)
	if !ok {
		return false
	}
	next := make([]any, 0, len(existing))
	removed := false
	for _, item := range existing {
		if customModelID(item) == modelID {
			removed = true
			continue
		}
		next = append(next, item)
	}
	if removed {
		providerConfig["models"] = next
	}
	return removed
}

// customModelCount returns how many model entries remain on a provider.
func customModelCount(providerConfig map[string]any) int {
	if arr, ok := providerConfig["models"].([]any); ok {
		return len(arr)
	}
	return 0
}

// clearRolesReferencingModel deletes any agents config that binds a role to
// fullModelID and returns the affected modelrole role names, so the caller can
// reset the live registry. Covers the flat agents.{default,lightweight,fallback}Model
// fields (PersistRoleModel's mapping) and the nested agents.defaults.model main
// fallback (resolveDefaultModel reads it when agents.defaultModel is absent).
func clearRolesReferencingModel(raw map[string]any, fullModelID string) []string {
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return nil
	}
	var cleared []string
	mainCleared := false
	for _, f := range []struct{ field, role string }{
		{"defaultModel", "main"},
		{"lightweightModel", "lightweight"},
		{"fallbackModel", "fallback"},
	} {
		if cur, ok := agents[f.field].(string); ok && strings.TrimSpace(cur) == fullModelID {
			delete(agents, f.field)
			cleared = append(cleared, f.role)
			if f.role == "main" {
				mainCleared = true
			}
		}
	}
	// Nested agents.defaults.model is the main-model fallback when the flat
	// field is absent; clear it too so main never resolves to the deleted model.
	if defaults, ok := agents["defaults"].(map[string]any); ok {
		if clearDefaultsModelIfMatches(defaults, fullModelID) && !mainCleared {
			cleared = append(cleared, "main")
		}
	}
	return cleared
}

// clearDefaultsModelIfMatches removes agents.defaults.model when it points at
// fullModelID, accepting either the string form ("provider/model") or the
// object form ({"primary": "provider/model", ...}). Returns whether it cleared.
func clearDefaultsModelIfMatches(defaults map[string]any, fullModelID string) bool {
	switch m := defaults["model"].(type) {
	case string:
		if strings.TrimSpace(m) == fullModelID {
			delete(defaults, "model")
			return true
		}
	case map[string]any:
		if primary, ok := m["primary"].(string); ok && strings.TrimSpace(primary) == fullModelID {
			delete(defaults, "model")
			return true
		}
	}
	return false
}

// mergeAuthConfig merges an override auth config into the base.
func mergeAuthConfig(base, override *GatewayAuthConfig) *GatewayAuthConfig {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}
	merged := *base
	if override.Mode != "" {
		merged.Mode = override.Mode
	}
	if override.Token != "" {
		merged.Token = override.Token
	}
	if override.Password != "" {
		merged.Password = override.Password
	}
	if override.AllowTailscale != nil {
		merged.AllowTailscale = override.AllowTailscale
	}
	if override.RateLimit != nil {
		merged.RateLimit = override.RateLimit
	}
	if override.TrustedProxy != nil {
		merged.TrustedProxy = override.TrustedProxy
	}
	return &merged
}

// mergeTailscaleConfig merges an override tailscale config into the base.
func mergeTailscaleConfig(base, override *GatewayTailscaleConfig) *GatewayTailscaleConfig {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}
	merged := *base
	if override.Mode != "" {
		merged.Mode = override.Mode
	}
	if override.ResetOnExit != nil {
		merged.ResetOnExit = override.ResetOnExit
	}
	return &merged
}

// ResolveMediaCleanupTTLMs resolves the media cleanup TTL from hours to milliseconds.
// Bounds: 1 hour minimum, 168 hours (7 days) maximum.
func ResolveMediaCleanupTTLMs(ttlHours int) (int64, error) {
	if ttlHours < 1 {
		ttlHours = 1
	}
	if ttlHours > maxMediaTTLHours {
		ttlHours = maxMediaTTLHours
	}
	ttlMs := int64(ttlHours) * 60 * 60_000
	if ttlMs <= 0 {
		return 0, fmt.Errorf("invalid media.ttlHours: %d", ttlHours)
	}
	return ttlMs, nil
}
