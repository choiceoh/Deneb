package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// DefaultGatewayPort is the default gateway server port.
	DefaultGatewayPort = 18789
	// DefaultStateDirname is the canonical state directory name.
	DefaultStateDirname = ".deneb"
	// DefaultConfigFilename is the canonical config file name.
	DefaultConfigFilename = "deneb.json"
)

// Legacy directory and config file names for migration support.
var (
	legacyStateDirnames   = []string{".clawdbot", ".moldbot", ".moltbot"}
	legacyConfigFilenames = []string{"clawdbot.json", "moldbot.json", "moltbot.json"}
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

// ResolveStateDir determines the Deneb state directory.
// Priority: DENEB_STATE_DIR env > existing legacy dir > ~/.deneb
func ResolveStateDir() string {
	if override := strings.TrimSpace(os.Getenv("DENEB_STATE_DIR")); override != "" {
		return expandHomePath(override)
	}
	if override := strings.TrimSpace(os.Getenv("CLAWDBOT_STATE_DIR")); override != "" {
		return expandHomePath(override)
	}

	home := resolveHomeDir()
	newDir := filepath.Join(home, DefaultStateDirname)
	if dirExists(newDir) {
		return newDir
	}
	for _, legacy := range legacyStateDirnames {
		legacyDir := filepath.Join(home, legacy)
		if dirExists(legacyDir) {
			return legacyDir
		}
	}
	return newDir
}

// ResolveConfigPath determines the config file path.
// Priority: DENEB_CONFIG_PATH env > existing config in state dir > canonical path.
func ResolveConfigPath() string {
	if override := strings.TrimSpace(os.Getenv("DENEB_CONFIG_PATH")); override != "" {
		return expandHomePath(override)
	}
	if override := strings.TrimSpace(os.Getenv("CLAWDBOT_CONFIG_PATH")); override != "" {
		return expandHomePath(override)
	}

	stateDir := ResolveStateDir()

	// Check for existing config files in state dir.
	candidates := []string{filepath.Join(stateDir, DefaultConfigFilename)}
	for _, legacy := range legacyConfigFilenames {
		candidates = append(candidates, filepath.Join(stateDir, legacy))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}

	// If state dir is default, also check legacy home dirs.
	home := resolveHomeDir()
	defaultStateDir := filepath.Join(home, DefaultStateDirname)
	if filepath.Clean(stateDir) == filepath.Clean(defaultStateDir) {
		for _, legacyDir := range legacyStateDirnames {
			dir := filepath.Join(home, legacyDir)
			allNames := append([]string{DefaultConfigFilename}, legacyConfigFilenames...)
			for _, name := range allNames {
				candidate := filepath.Join(dir, name)
				if fileExists(candidate) {
					return candidate
				}
			}
		}
	}

	return filepath.Join(stateDir, DefaultConfigFilename)
}

// ResolveGatewayPort determines the gateway port from env or config.
func ResolveGatewayPort(cfg *DenebConfig) int {
	if envRaw := strings.TrimSpace(os.Getenv("DENEB_GATEWAY_PORT")); envRaw != "" {
		var port int
		if _, err := fmt.Sscanf(envRaw, "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	if envRaw := strings.TrimSpace(os.Getenv("CLAWDBOT_GATEWAY_PORT")); envRaw != "" {
		var port int
		if _, err := fmt.Sscanf(envRaw, "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	if cfg != nil && cfg.Gateway != nil && cfg.Gateway.Port != nil && *cfg.Gateway.Port > 0 {
		return *cfg.Gateway.Port
	}
	return DefaultGatewayPort
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

// supportedChannels lists channel IDs that the gateway actually implements.
// Config entries for unknown channels are silently ignored so stale config
// keys (e.g., removed plugins) don't appear in the startup banner.
var supportedChannels = map[string]bool{
	"telegram": true,
}

// ConfiguredChannelIDs extracts the top-level keys from the "channels"
// object in the raw config JSON, filtered to only channels the gateway
// actually supports. Used for lazy loading: only configured channels are
// started at boot.
func ConfiguredChannelIDs(snap *ConfigSnapshot) []string {
	if snap == nil || snap.Raw == "" {
		return nil
	}
	var raw struct {
		Channels map[string]json.RawMessage `json:"channels"`
	}
	if err := json.Unmarshal([]byte(snap.Raw), &raw); err != nil {
		return nil
	}
	if len(raw.Channels) == 0 {
		return nil
	}
	ids := make([]string, 0, len(raw.Channels))
	for k := range raw.Channels {
		if supportedChannels[k] {
			ids = append(ids, k)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
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

// hashRaw computes a SHA-256 hex hash of the raw config bytes (or "empty" for nil).
func hashRaw(data []byte) string {
	if data == nil {
		h := sha256.Sum256([]byte(""))
		return hex.EncodeToString(h[:])
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func resolveHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if runtime.GOOS == "windows" {
			return `C:\`
		}
		return "/tmp"
	}
	return home
}

func expandHomePath(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(resolveHomeDir(), p[2:])
	}
	return p
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ResolveAgentWorkspaceDir determines the workspace directory for the default agent.
// Priority:
//  1. agents.list[] entry with default=true → workspace
//  2. agents.defaults.workspace
//  3. ~/.deneb/workspace (built-in default, matching TS resolveDefaultAgentWorkspaceDir)
func ResolveAgentWorkspaceDir(cfg *DenebConfig) string {
	if cfg != nil && cfg.Agents != nil {
		// Check per-agent workspace from agents.list[].
		for _, agent := range cfg.Agents.List {
			if agent.Default != nil && *agent.Default && strings.TrimSpace(agent.Workspace) != "" {
				return expandHomePath(strings.TrimSpace(agent.Workspace))
			}
		}
		// Check agents.defaults.workspace.
		if cfg.Agents.Defaults != nil && strings.TrimSpace(cfg.Agents.Defaults.Workspace) != "" {
			return expandHomePath(strings.TrimSpace(cfg.Agents.Defaults.Workspace))
		}
	}

	// Built-in default: ~/.deneb/workspace (matches TS resolveDefaultAgentWorkspaceDir).
	home := resolveHomeDir()
	profile := strings.TrimSpace(os.Getenv("DENEB_PROFILE"))
	if profile != "" && strings.ToLower(profile) != "default" {
		return filepath.Join(home, DefaultStateDirname, "workspace-"+profile)
	}
	return filepath.Join(home, DefaultStateDirname, "workspace")
}
