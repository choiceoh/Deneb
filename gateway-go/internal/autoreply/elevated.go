// elevated.go — Elevated execution mode and allowlist matching.
// Mirrors src/auto-reply/reply/reply-elevated.ts (237 LOC),
// elevated-allowlist-matcher.ts (156 LOC), elevated-unavailable.ts (28 LOC),
// command-gates.ts (88 LOC), bash-command.ts (406 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"regexp"
	"strings"
)

// ElevatedModeAvailable checks if elevated execution is available.
func ElevatedModeAvailable(session *types.SessionState) bool {
	return session != nil && session.ElevatedLevel != types.ElevatedOff
}

// ElevatedUnavailableMessage returns a message when elevated mode is not available.
func ElevatedUnavailableMessage() string {
	return "⚠️ Elevated execution is not available in this session. Use /elevated on to enable."
}

// AllowlistEntry describes a single allowlist pattern.
type AllowlistEntry struct {
	Pattern string `json:"pattern"`
	IsRegex bool   `json:"isRegex,omitempty"`
}

// AllowlistMatcher checks commands against an allowlist.
type AllowlistMatcher struct {
	entries  []AllowlistEntry
	compiled []*regexp.Regexp
}

// NewAllowlistMatcher creates a matcher from allowlist entries.
func NewAllowlistMatcher(entries []AllowlistEntry) *AllowlistMatcher {
	m := &AllowlistMatcher{entries: entries}
	for _, e := range entries {
		if e.IsRegex {
			if re, err := regexp.Compile(e.Pattern); err == nil {
				m.compiled = append(m.compiled, re)
			}
		}
	}
	return m
}

// IsAllowed returns true if the command matches an allowlist entry.
func (m *AllowlistMatcher) IsAllowed(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}

	for _, e := range m.entries {
		if !e.IsRegex {
			if matchGlob(trimmed, e.Pattern) {
				return true
			}
		}
	}

	for _, re := range m.compiled {
		if re.MatchString(trimmed) {
			return true
		}
	}

	return false
}

// matchGlob performs simple glob matching (* only).
func matchGlob(text, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return strings.EqualFold(text, pattern)
	}
	// Split on * and check each part.
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(strings.ToLower(text[pos:]), strings.ToLower(part))
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false // must match from start if no leading *
		}
		pos += idx + len(part)
	}
	if parts[len(parts)-1] != "" && pos != len(text) {
		return false // must match to end if no trailing *
	}
	return true
}

// CommandGate controls which commands are allowed based on scope.
type CommandGate struct {
	AllowBash    bool
	AllowConfig  bool
	AllowPlugins bool
	AllowDebug   bool
	AllowMCP     bool
}

// DefaultCommandGate returns the default gate configuration.
func DefaultCommandGate() CommandGate {
	return CommandGate{
		AllowBash:    true,
		AllowConfig:  true,
		AllowPlugins: true,
		AllowDebug:   false,
		AllowMCP:     true,
	}
}

// IsCommandGated returns true if the command is blocked by the gate.
func (g CommandGate) IsCommandGated(command string) bool {
	switch command {
	case "bash", "sh":
		return !g.AllowBash
	case "config":
		return !g.AllowConfig
	case "plugins", "plugin":
		return !g.AllowPlugins
	case "debug":
		return !g.AllowDebug
	case "mcp":
		return !g.AllowMCP
	}
	return false
}

// BashCommandConfig configures bash command execution.
type BashCommandConfig struct {
	Enabled         bool
	Allowlist       *AllowlistMatcher
	RequireApproval bool
	Timeout         int64 // milliseconds
}

// DefaultBashConfig returns the default bash command configuration.
func DefaultBashConfig() BashCommandConfig {
	return BashCommandConfig{
		Enabled:         true,
		RequireApproval: true,
		Timeout:         30000, // 30 seconds
	}
}

// ValidateBashCommand checks if a bash command is allowed to execute.
func ValidateBashCommand(command string, cfg BashCommandConfig) (allowed bool, reason string) {
	if !cfg.Enabled {
		return false, "Bash execution is disabled."
	}
	if cfg.Allowlist != nil && !cfg.Allowlist.IsAllowed(command) {
		return false, "Command not in allowlist."
	}
	return true, ""
}
