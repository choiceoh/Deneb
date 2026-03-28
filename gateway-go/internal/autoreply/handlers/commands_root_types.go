// commands_root_types.go — Types that are still in the autoreply root package
// but needed by the commands subpackage.
// Uses the session/ subpackage for types already extracted there.
// The remaining types (AllowlistMatcher, BashCommandConfig) are defined here
// because they have not yet been extracted to their own subpackage.
// The root package's compat file (commands_compat.go) will type-alias
// these back so external callers see a single canonical type.
//
// TODO: As AllowlistMatcher/BashCommandConfig are extracted to their own
// subpackage, delete them from here and import that subpackage directly.
package handlers

import (
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
)

// --- From session/ subpackage (used via import, not duplicated) ---

// AbortCutoffContext identifies a specific message used as abort cutoff.
// Re-exported from session.AbortCutoffContext.
type AbortCutoffContext = session.AbortCutoffContext

// SessionUsage tracks detailed token usage for a session.
// Re-exported from session.SessionUsage.
type SessionUsage = session.SessionUsage

// currentTimeMs returns the current time in milliseconds since epoch.
var currentTimeMs = func() int64 {
	return time.Now().UnixMilli()
}

// FormatTimestampWithAge formats a millisecond timestamp as ISO 8601 with relative age.
// Delegates to session.FormatTimestampWithAge.
var FormatTimestampWithAge = session.FormatTimestampWithAge

// --- From elevated.go (not yet extracted to its own subpackage) ---

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

func matchGlob(text, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return strings.EqualFold(text, pattern)
	}
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
			return false
		}
		pos += idx + len(part)
	}
	if parts[len(parts)-1] != "" && pos != len(text) {
		return false
	}
	return true
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
		Timeout:         30000,
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

// ElevatedUnavailableMessage returns a message when elevated mode is not available.
func ElevatedUnavailableMessage() string {
	return "⚠️ Elevated execution is not available in this session. Use /elevated on to enable."
}
