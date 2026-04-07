package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// generatedTokenBytes is the number of random bytes for auto-generated tokens (24 bytes = 48 hex chars).
	generatedTokenBytes = 24
	// maxMediaTTLHours is the maximum media retention window (7 days).
	maxMediaTTLHours = 24 * 7
)

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
