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
			case "auto", "lan", "loopback", "custom", "tailnet":
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.bind",
					Message: fmt.Sprintf("invalid bind mode %q (expected auto|lan|loopback|custom|tailnet)", gw.Bind),
				})
			}
		}

		// Validate custom bind host.
		if gw.Bind == "custom" && strings.TrimSpace(gw.CustomBindHost) == "" {
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
			case "none", "token", "password", "trusted-proxy":
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
			case "off", "serve", "funnel":
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
			case "off", "restart", "hot", "hybrid":
				// Valid.
			default:
				issues = append(issues, ConfigIssue{
					Path:    "gateway.reload.mode",
					Message: fmt.Sprintf("invalid reload mode %q", gw.Reload.Mode),
				})
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
		cfg.Gateway.Bind = "loopback"
	}
	if cfg.Gateway.Auth == nil {
		cfg.Gateway.Auth = &GatewayAuthConfig{}
	}
	if cfg.Gateway.Auth.Mode == "" {
		cfg.Gateway.Auth.Mode = "token"
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
		cfg.Gateway.Tailscale.Mode = "off"
	}
	if cfg.Gateway.ChannelHealthCheckMinutes == nil {
		v := 5
		cfg.Gateway.ChannelHealthCheckMinutes = &v
	}
	if cfg.Gateway.ChannelStaleEventThresholdMinutes == nil {
		v := 30
		cfg.Gateway.ChannelStaleEventThresholdMinutes = &v
	}
	if cfg.Gateway.ChannelMaxRestartsPerHour == nil {
		v := 10
		cfg.Gateway.ChannelMaxRestartsPerHour = &v
	}
	if cfg.Gateway.Reload == nil {
		cfg.Gateway.Reload = &GatewayReloadConfig{}
	}
	if cfg.Gateway.Reload.Mode == "" {
		cfg.Gateway.Reload.Mode = "hybrid"
	}
	if cfg.Gateway.Reload.DebounceMs == nil {
		v := 300
		cfg.Gateway.Reload.DebounceMs = &v
	}
	if cfg.Gateway.Reload.DeferralTimeoutMs == nil {
		v := 300000
		cfg.Gateway.Reload.DeferralTimeoutMs = &v
	}

	// Auth rate limit defaults.
	if cfg.Gateway.Auth.RateLimit == nil {
		cfg.Gateway.Auth.RateLimit = &GatewayAuthRateLimitConfig{}
	}
	rl := cfg.Gateway.Auth.RateLimit
	if rl.MaxAttempts == nil {
		v := 10
		rl.MaxAttempts = &v
	}
	if rl.WindowMs == nil {
		v := 60000
		rl.WindowMs = &v
	}
	if rl.LockoutMs == nil {
		v := 300000
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
		cfg.Session.MainKey = "main"
	}

	// Agent defaults.
	if cfg.Agents == nil {
		cfg.Agents = &AgentsConfig{}
	}
	if cfg.Agents.MaxConcurrent == nil {
		v := 8
		cfg.Agents.MaxConcurrent = &v
	}
	if cfg.Agents.SubagentMaxConcurrent == nil {
		v := 2
		cfg.Agents.SubagentMaxConcurrent = &v
	}

	// Logging defaults.
	if cfg.Logging == nil {
		cfg.Logging = &LoggingConfig{}
	}
	if cfg.Logging.RedactSensitive == "" {
		cfg.Logging.RedactSensitive = "tools"
	}
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
