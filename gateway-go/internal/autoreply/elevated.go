// elevated.go — Elevated execution mode helpers.
// AllowlistMatcher, BashCommandConfig, and related types have moved to
// internal/autoreply/commands/ and are re-exported via commands_compat.go.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ElevatedModeAvailable checks if elevated execution is available.
func ElevatedModeAvailable(session *types.SessionState) bool {
	return session != nil && session.ElevatedLevel != types.ElevatedOff
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
