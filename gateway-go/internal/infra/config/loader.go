package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ConfigSnapshot holds the result of loading and validating a config file.
type ConfigSnapshot struct {
	Path     string        `json:"path"`
	Exists   bool          `json:"exists"`
	Raw      string        `json:"raw,omitempty"`
	Config   DenebConfig   `json:"config"`
	Hash     string        `json:"hash"`
	Valid    bool          `json:"valid"`
	Issues   []ConfigIssue `json:"issues,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

// ConfigIssue represents a config validation error.
type ConfigIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (i ConfigIssue) String() string {
	if i.Path == "" {
		return i.Message
	}
	return fmt.Sprintf("%s: %s", i.Path, i.Message)
}

// LoadConfig reads and parses the Deneb config file, returning a snapshot.
func LoadConfig(configPath string) (*ConfigSnapshot, error) {
	snap := &ConfigSnapshot{
		Path:  configPath,
		Valid: true,
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snap.Exists = false
			snap.Hash = hashRaw(nil)
			applyDefaults(&snap.Config)
			return snap, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", configPath, err)
	}

	snap.Exists = true
	snap.Raw = string(raw)
	snap.Hash = hashRaw(raw)

	var cfg DenebConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		snap.Valid = false
		snap.Issues = append(snap.Issues, ConfigIssue{
			Path:    "",
			Message: fmt.Sprintf("JSON parse failed: %s", err),
		})
		return snap, nil
	}

	snap.Config = cfg

	// Validate and collect issues.
	issues, warnings := validateConfig(&cfg)
	snap.Issues = issues
	snap.Warnings = warnings
	snap.Valid = len(issues) == 0

	// Apply defaults to fill missing values.
	applyDefaults(&snap.Config)

	return snap, nil
}

// LoadConfigFromDefaultPath loads config from the auto-resolved path.
func LoadConfigFromDefaultPath() (*ConfigSnapshot, error) {
	return LoadConfig(ResolveConfigPath())
}

// validateConfig performs basic structural validation on the config.
func validateConfig(cfg *DenebConfig) (issues []ConfigIssue, warnings []string) {
	if cfg.Gateway != nil {
		gw := cfg.Gateway

		// Validate bind mode.
		if gw.Bind != "" {
			switch gw.Bind {
			case BindAuto, BindLAN, BindLoopback, BindCustom, BindTailnet:
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.bind",
					Message: fmt.Sprintf("invalid bind mode %q (expected auto|lan|loopback|custom|tailnet)", gw.Bind),
				})
			}
		}

		// Validate custom bind host.
		if gw.Bind == BindCustom && strings.TrimSpace(gw.CustomBindHost) == "" {
			issues = append(issues, ConfigIssue{
				Path:    "gateway.customBindHost",
				Message: "gateway.bind=custom requires gateway.customBindHost",
			})
		}

		// Validate port range.
		if gw.Port != nil {
			port := *gw.Port
			if port < 1 || port > 65535 {
				issues = append(issues, ConfigIssue{
					Path:    "gateway.port",
					Message: fmt.Sprintf("port %d out of range (1-65535)", port),
				})
			}
		}

		// Validate auth mode.
		if gw.Auth != nil && gw.Auth.Mode != "" {
			switch gw.Auth.Mode {
			case AuthModeNone, AuthModeToken, AuthModePassword, AuthModeTrustedProxy:
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.auth.mode",
					Message: fmt.Sprintf("invalid auth mode %q", gw.Auth.Mode),
				})
			}
		}

		// Validate tailscale mode.
		if gw.Tailscale != nil && gw.Tailscale.Mode != "" {
			switch gw.Tailscale.Mode {
			case TailscaleOff, TailscaleServe, TailscaleFunnel:
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.tailscale.mode",
					Message: fmt.Sprintf("invalid tailscale mode %q", gw.Tailscale.Mode),
				})
			}
		}

		// Validate reload mode.
		if gw.Reload != nil && gw.Reload.Mode != "" {
			switch gw.Reload.Mode {
			case ReloadOff, ReloadRestart, ReloadHot, ReloadHybrid:
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.reload.mode",
					Message: fmt.Sprintf("invalid reload mode %q", gw.Reload.Mode),
				})
			}
		}
	}

	// Validate hooks.
	if cfg.Hooks != nil {
		seenIDs := make(map[string]struct{})
		for i, hook := range cfg.Hooks.Entries {
			prefix := fmt.Sprintf("hooks.entries[%d]", i)

			if strings.TrimSpace(hook.Event) == "" {
				issues = append(issues, ConfigIssue{
					Path:    prefix + ".event",
					Message: "hook entry requires a non-empty event",
				})
			}

			if strings.TrimSpace(hook.Command) == "" {
				issues = append(issues, ConfigIssue{
					Path:    prefix + ".command",
					Message: "hook entry requires a non-empty command",
				})
			}

			if hook.TimeoutMs != nil && *hook.TimeoutMs < 0 {
				issues = append(issues, ConfigIssue{
					Path:    prefix + ".timeoutMs",
					Message: fmt.Sprintf("timeoutMs must be non-negative (got %d)", *hook.TimeoutMs),
				})
			}
			if hook.TimeoutMs != nil && *hook.TimeoutMs > DefaultMaxHookTimeoutMs {
				warnings = append(warnings, fmt.Sprintf(
					"%s.timeoutMs: %d exceeds recommended maximum (%d ms)",
					prefix, *hook.TimeoutMs, DefaultMaxHookTimeoutMs,
				))
			}

			if hook.ID != "" {
				if _, ok := seenIDs[hook.ID]; ok {
					issues = append(issues, ConfigIssue{
						Path:    prefix + ".id",
						Message: fmt.Sprintf("duplicate hook ID %q", hook.ID),
					})
				}
				seenIDs[hook.ID] = struct{}{}
			}
		}
	}

	return issues, warnings
}

// applyDefaults fills in default values for missing config fields.
func applyDefaults(cfg *DenebConfig) {
	// Gateway defaults.
	if cfg.Gateway == nil {
		cfg.Gateway = &GatewayConfig{}
	}
	if cfg.Gateway.Port == nil {
		port := DefaultGatewayPort
		cfg.Gateway.Port = &port
	}
	if cfg.Gateway.Bind == "" {
		cfg.Gateway.Bind = BindLoopback
	}
	if cfg.Gateway.Auth == nil {
		cfg.Gateway.Auth = &GatewayAuthConfig{}
	}
	if cfg.Gateway.Auth.Mode == "" {
		cfg.Gateway.Auth.Mode = AuthModeToken
	}
	if cfg.Gateway.ControlUI == nil {
		cfg.Gateway.ControlUI = &GatewayControlUIConfig{}
	}
	if cfg.Gateway.ControlUI.Enabled == nil {
		enabled := true
		cfg.Gateway.ControlUI.Enabled = &enabled
	}
	if cfg.Gateway.Tailscale == nil {
		cfg.Gateway.Tailscale = &GatewayTailscaleConfig{}
	}
	if cfg.Gateway.Tailscale.Mode == "" {
		cfg.Gateway.Tailscale.Mode = TailscaleOff
	}
	if cfg.Gateway.ChannelHealthCheckMinutes == nil {
		v := DefaultChannelHealthCheckMinutes
		cfg.Gateway.ChannelHealthCheckMinutes = &v
	}
	if cfg.Gateway.ChannelStaleEventThresholdMinutes == nil {
		v := DefaultChannelStaleThresholdMinutes
		cfg.Gateway.ChannelStaleEventThresholdMinutes = &v
	}
	if cfg.Gateway.ChannelMaxRestartsPerHour == nil {
		v := DefaultChannelMaxRestartsPerHour
		cfg.Gateway.ChannelMaxRestartsPerHour = &v
	}
	if cfg.Gateway.Reload == nil {
		cfg.Gateway.Reload = &GatewayReloadConfig{}
	}
	if cfg.Gateway.Reload.Mode == "" {
		cfg.Gateway.Reload.Mode = ReloadHybrid
	}
	if cfg.Gateway.Reload.DebounceMs == nil {
		v := DefaultReloadDebounceMs
		cfg.Gateway.Reload.DebounceMs = &v
	}
	if cfg.Gateway.Reload.DeferralTimeoutMs == nil {
		v := DefaultReloadDeferralTimeoutMs
		cfg.Gateway.Reload.DeferralTimeoutMs = &v
	}

	// Auth rate limit defaults.
	if cfg.Gateway.Auth.RateLimit == nil {
		cfg.Gateway.Auth.RateLimit = &GatewayAuthRateLimitConfig{}
	}
	rl := cfg.Gateway.Auth.RateLimit
	if rl.MaxAttempts == nil {
		v := DefaultAuthRateLimitMaxAttempts
		rl.MaxAttempts = &v
	}
	if rl.WindowMs == nil {
		v := DefaultAuthRateLimitWindowMs
		rl.WindowMs = &v
	}
	if rl.LockoutMs == nil {
		v := DefaultAuthRateLimitLockoutMs
		rl.LockoutMs = &v
	}
	if rl.ExemptLoopback == nil {
		v := true
		rl.ExemptLoopback = &v
	}

	// Session defaults.
	if cfg.Session == nil {
		cfg.Session = &SessionConfig{}
	}
	if cfg.Session.MainKey == "" {
		cfg.Session.MainKey = DefaultSessionMainKey
	}

	// Agent defaults.
	if cfg.Agents == nil {
		cfg.Agents = &AgentsConfig{}
	}
	if cfg.Agents.MaxConcurrent == nil {
		v := DefaultAgentMaxConcurrent
		cfg.Agents.MaxConcurrent = &v
	}
	if cfg.Agents.SubagentMaxConcurrent == nil {
		v := DefaultSubagentMaxConcurrent
		cfg.Agents.SubagentMaxConcurrent = &v
	}

	// Logging defaults.
	if cfg.Logging == nil {
		cfg.Logging = &LoggingConfig{}
	}
	if cfg.Logging.RedactSensitive == "" {
		cfg.Logging.RedactSensitive = DefaultLogRedactSensitive
	}
}

// ValidateRawConfig parses and validates raw JSON config bytes, returning any issues.
func ValidateRawConfig(raw []byte) (issues []ConfigIssue, warnings []string, err error) {
	var cfg DenebConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return []ConfigIssue{{Message: "JSON parse failed: " + err.Error()}}, nil, nil //nolint:nilerr // parse error reported as config issue, not Go error
	}
	issues, warnings = validateConfig(&cfg)
	return issues, warnings, nil
}

// hashRaw computes a SHA-256 hex hash of the raw config bytes (or "" for nil).
func hashRaw(data []byte) string {
	if data == nil {
		h := sha256.Sum256([]byte(""))
		return hex.EncodeToString(h[:])
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
