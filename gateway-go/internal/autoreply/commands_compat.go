// commands_compat.go — temporary shims for symbols extracted into
// autoreply/handlers and autoreply/rules.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/rules"
)

// CommandRegistry aliases handlers.CommandRegistry for backward compatibility.
type CommandRegistry = handlers.CommandRegistry

// CommandRouter aliases handlers.CommandRouter for backward compatibility.
type CommandRouter = handlers.CommandRouter

// CommandContext aliases handlers.CommandContext for backward compatibility.
type CommandContext = handlers.CommandContext

// CommandResult aliases handlers.CommandResult for backward compatibility.
type CommandResult = handlers.CommandResult

// CommandDeps aliases handlers.CommandDeps for backward compatibility.
type CommandDeps = handlers.CommandDeps

// StatusDeps aliases handlers.StatusDeps for backward compatibility.
type StatusDeps = handlers.StatusDeps

// ProviderUsageStats aliases handlers.ProviderUsageStats for backward compatibility.
type ProviderUsageStats = handlers.ProviderUsageStats

// ChannelHealthEntry aliases handlers.ChannelHealthEntry for backward compatibility.
type ChannelHealthEntry = handlers.ChannelHealthEntry

// NewCommandRegistry forwards to handlers.NewCommandRegistry.
func NewCommandRegistry(commands []handlers.ChatCommandDefinition) *CommandRegistry {
	return handlers.NewCommandRegistry(commands)
}

// NewCommandRouter forwards to handlers.NewCommandRouter.
func NewCommandRouter(registry *CommandRegistry) *CommandRouter {
	return handlers.NewCommandRouter(registry)
}

// BuiltinChatCommands forwards to handlers.BuiltinChatCommands.
func BuiltinChatCommands() []handlers.ChatCommandDefinition {
	return handlers.BuiltinChatCommands()
}

// ParseInlineDirectives forwards to rules.ParseInlineDirectives.
func ParseInlineDirectives(body string, opts *rules.DirectiveParseOptions) rules.InlineDirectives {
	return rules.ParseInlineDirectives(body, opts)
}
