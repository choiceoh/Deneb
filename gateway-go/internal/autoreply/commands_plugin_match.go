// commands_plugin_match.go — Plugin command matching and execution.
// Mirrors src/auto-reply/reply/commands-plugin.ts (53 LOC).
package autoreply

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
)

// PluginCommandMatch holds a matched plugin command and its parsed args.
type PluginCommandMatch struct {
	Command plugin.CommandRegistration
	Args    []string
}

// MatchPluginCommand checks if the normalized command body matches a
// registered plugin command. Returns nil if no match.
func MatchPluginCommand(body string, registry *plugin.FullRegistry) *PluginCommandMatch {
	if body == "" || registry == nil {
		return nil
	}
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	// Split into command name and arguments.
	parts := strings.SplitN(trimmed[1:], " ", 2)
	cmdName := strings.ToLower(parts[0])
	var args []string
	if len(parts) > 1 {
		argStr := strings.TrimSpace(parts[1])
		if argStr != "" {
			args = strings.Fields(argStr)
		}
	}

	// Look up in plugin registry.
	for _, cmd := range registry.ListCommands() {
		if strings.ToLower(cmd.Name) == cmdName {
			return &PluginCommandMatch{Command: cmd, Args: args}
		}
	}
	return nil
}

// ExecutePluginCommand runs a matched plugin command.
// The handler is responsible for side effects (replies, state changes);
// this function only surfaces errors.
func ExecutePluginCommand(ctx context.Context, match *PluginCommandMatch) (string, error) {
	if match == nil || match.Command.Handler == nil {
		return "", nil
	}
	if err := match.Command.Handler(ctx, match.Args); err != nil {
		return "⚠️ Plugin command error: " + err.Error(), err
	}
	// Handler ran successfully; the handler itself is responsible for
	// delivering replies through its own channel (e.g. sendReply callback).
	// Return empty text — the caller should treat this as "handled, no inline reply".
	return "", nil
}

// HandlePluginCommandInPipeline is the pipeline handler that checks for
// plugin commands before the LLM agent. Returns (reply, handled).
func HandlePluginCommandInPipeline(ctx context.Context, body string, allowTextCommands bool, registry *plugin.FullRegistry) (string, bool) {
	if !allowTextCommands {
		return "", false
	}
	match := MatchPluginCommand(body, registry)
	if match == nil {
		return "", false
	}
	reply, _ := ExecutePluginCommand(ctx, match)
	return reply, true
}
