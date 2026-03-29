// Package config — path and port resolution policies.
//
// Each policy type encodes one resolution concern together with its
// environment-variable names and hardcoded defaults.  Keeping the precedence
// rules in a struct makes them visible, independently testable, and easy to
// update when legacy names are retired.
//
// The public free functions (ResolveStateDir, ResolveConfigPath,
// ResolveGatewayPort) are thin wrappers that call the default policy; prefer
// them in application code.
package config

import (
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

// ── StateDirPolicy ────────────────────────────────────────────────────────────

// StateDirPolicy encodes the precedence rules for resolving the state directory.
//
// Precedence (first match wins):
//  1. EnvVars[0] (DENEB_STATE_DIR)
//  2. EnvVars[1] (CLAWDBOT_STATE_DIR, legacy)
//  3. ~/.deneb if it already exists on disk
//  4. First existing legacy directory (.clawdbot, .moldbot, .moltbot)
//  5. ~/.deneb (default fallback — directory need not exist)
type StateDirPolicy struct {
	// EnvVars are checked in order for an explicit path override.
	EnvVars []string
	// Dirname is the canonical state directory name (e.g. ".deneb").
	Dirname string
}

// DefaultStateDirPolicy returns the standard production policy.
func DefaultStateDirPolicy() StateDirPolicy {
	return StateDirPolicy{
		EnvVars: []string{"DENEB_STATE_DIR", "CLAWDBOT_STATE_DIR"},
		Dirname: DefaultStateDirname,
	}
}

// ResolveFrom resolves the state directory against an explicit home path.
// Useful in tests where the real home directory must not be consulted.
func (p StateDirPolicy) ResolveFrom(home string) string {
	// 1–2. Env override.
	for _, key := range p.EnvVars {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return expandHomePath(v)
		}
	}

	newDir := filepath.Join(home, p.Dirname)

	// 3. Canonical dir exists.
	if dirExists(newDir) {
		return newDir
	}

	// 4. First existing legacy dir.
	if legacy := findLegacyStateDir(home); legacy != "" {
		return legacy
	}

	// 5. Default.
	return newDir
}

// Resolve resolves using the current process home directory.
func (p StateDirPolicy) Resolve() string {
	return p.ResolveFrom(resolveHomeDir())
}

// ── ConfigPathPolicy ──────────────────────────────────────────────────────────

// ConfigPathPolicy encodes the precedence rules for resolving the config path.
//
// Precedence (first match wins):
//  1. EnvVars[0] (DENEB_CONFIG_PATH)
//  2. EnvVars[1] (CLAWDBOT_CONFIG_PATH, legacy)
//  3. First existing config candidate in stateDir (canonical, then legacy names)
//  4. {stateDir}/deneb.json (default fallback — file need not exist)
type ConfigPathPolicy struct {
	// EnvVars are checked in order for an explicit path override.
	EnvVars []string
	// Filename is the canonical config filename (e.g. "deneb.json").
	Filename string
}

// DefaultConfigPathPolicy returns the standard production policy.
func DefaultConfigPathPolicy() ConfigPathPolicy {
	return ConfigPathPolicy{
		EnvVars:  []string{"DENEB_CONFIG_PATH", "CLAWDBOT_CONFIG_PATH"},
		Filename: DefaultConfigFilename,
	}
}

// ResolveFrom resolves the config path given an explicit stateDir.
// Useful in tests where the state directory is known up front.
func (p ConfigPathPolicy) ResolveFrom(stateDir string) string {
	// 1–2. Env override.
	for _, key := range p.EnvVars {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return expandHomePath(v)
		}
	}

	// 3. First existing candidate.
	for _, candidate := range p.Candidates(stateDir) {
		if fileExists(candidate) {
			return candidate
		}
	}

	// 4. Default.
	return filepath.Join(stateDir, p.Filename)
}

// Resolve resolves using the auto-resolved state directory.
func (p ConfigPathPolicy) Resolve() string {
	stateDir := DefaultStateDirPolicy().Resolve()

	// Also check legacy config files stored directly inside legacy home dirs
	// when the resolved state dir is the canonical default.
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

	return p.ResolveFrom(stateDir)
}

// Candidates returns the ordered list of config file paths to probe in stateDir.
func (p ConfigPathPolicy) Candidates(stateDir string) []string {
	out := make([]string, 0, 1+len(legacyConfigFilenames))
	out = append(out, filepath.Join(stateDir, p.Filename))
	for _, name := range legacyConfigFilenames {
		out = append(out, filepath.Join(stateDir, name))
	}
	return out
}

// ── GatewayPortPolicy ─────────────────────────────────────────────────────────

// GatewayPortPolicy encodes the precedence rules for resolving the gateway port.
//
// Precedence (first match wins):
//  1. EnvVars[0] (DENEB_GATEWAY_PORT)
//  2. EnvVars[1] (CLAWDBOT_GATEWAY_PORT, legacy)
//  3. Caller-supplied config port
//  4. DefaultPort (18789)
type GatewayPortPolicy struct {
	// EnvVars are checked in order for an explicit port override.
	EnvVars []string
	// DefaultPort is the built-in fallback port.
	DefaultPort int
}

// DefaultGatewayPortPolicy returns the standard production policy.
func DefaultGatewayPortPolicy() GatewayPortPolicy {
	return GatewayPortPolicy{
		EnvVars:     []string{"DENEB_GATEWAY_PORT", "CLAWDBOT_GATEWAY_PORT"},
		DefaultPort: DefaultGatewayPort,
	}
}

// ResolveFrom resolves from env vars and an optional explicit config port.
func (p GatewayPortPolicy) ResolveFrom(configPort *int) int {
	// 1–2. Env override.
	for _, key := range p.EnvVars {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			var port int
			if _, err := fmt.Sscanf(raw, "%d", &port); err == nil && port > 0 {
				return port
			}
		}
	}

	// 3. Config value.
	if configPort != nil && *configPort > 0 {
		return *configPort
	}

	// 4. Default.
	return p.DefaultPort
}

// Resolve extracts the port from a DenebConfig and delegates to ResolveFrom.
func (p GatewayPortPolicy) Resolve(cfg *DenebConfig) int {
	var configPort *int
	if cfg != nil && cfg.Gateway != nil && cfg.Gateway.Port != nil && *cfg.Gateway.Port > 0 {
		configPort = cfg.Gateway.Port
	}
	return p.ResolveFrom(configPort)
}

// ── Public free functions ──────────────────────────────────────────────────────

// ResolveStateDir determines the Deneb state directory using the default policy.
func ResolveStateDir() string {
	return DefaultStateDirPolicy().Resolve()
}

// ResolveConfigPath determines the config file path using the default policy.
func ResolveConfigPath() string {
	return DefaultConfigPathPolicy().Resolve()
}

// ResolveGatewayPort determines the gateway port using the default policy.
func ResolveGatewayPort(cfg *DenebConfig) int {
	return DefaultGatewayPortPolicy().Resolve(cfg)
}

// ResolveAgentWorkspaceDir determines the workspace directory for the default agent.
//
// Priority:
//  1. agents.list[] entry with default=true → workspace
//  2. agents.defaults.workspace
//  3. ~/.deneb/workspace (built-in default)
func ResolveAgentWorkspaceDir(cfg *DenebConfig) string {
	if cfg != nil && cfg.Agents != nil {
		// Per-agent workspace (default=true) takes highest priority.
		for _, agent := range cfg.Agents.List {
			if agent.Default != nil && *agent.Default && strings.TrimSpace(agent.Workspace) != "" {
				return expandHomePath(strings.TrimSpace(agent.Workspace))
			}
		}
		// agents.defaults.workspace.
		if cfg.Agents.Defaults != nil && strings.TrimSpace(cfg.Agents.Defaults.Workspace) != "" {
			return expandHomePath(strings.TrimSpace(cfg.Agents.Defaults.Workspace))
		}
	}

	// Built-in default: ~/.deneb/workspace.
	home := resolveHomeDir()
	profile := strings.TrimSpace(os.Getenv("DENEB_PROFILE"))
	if profile != "" && strings.ToLower(profile) != "default" {
		return filepath.Join(home, DefaultStateDirname, "workspace-"+profile)
	}
	return filepath.Join(home, DefaultStateDirname, "workspace")
}

// ── Helpers ────────────────────────────────────────────────────────────────────

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
