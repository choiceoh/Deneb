// commands_handlers_config.go — Config, set/unset, system-prompt, and debug command handlers.
package autoreply

import (
	"fmt"
	"strings"
)

func handleConfigCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "⚙️ Usage: /config <key> [value]\nUse /config to view, /set to set, /unset to remove.", SkipAgent: true}, nil
	}
	// Parse key=value or key value.
	key, value := parseSetUnset(raw)
	if value == "" {
		return &CommandResult{Reply: fmt.Sprintf("⚙️ Config `%s`: (current value)", key), SkipAgent: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("⚙️ Set `%s` = `%s`", key, value),
		SkipAgent: true,
	}, nil
}

func handleSetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /set <key> <value>", SkipAgent: true, IsError: true}, nil
	}
	key, value := parseSetUnset(raw)
	if value == "" {
		return &CommandResult{Reply: "⚠️ Usage: /set <key> <value>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("✅ Set `%s` = `%s`", key, value), SkipAgent: true}, nil
}

func handleUnsetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /unset <key>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("✅ Unset `%s`", strings.TrimSpace(raw)), SkipAgent: true}, nil
}

func handleSystemPromptCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{
			Reply:      "System prompt cleared.",
			SessionMod: &SessionModification{SystemPrompt: strPtr("")},
			SkipAgent:  true,
		}, nil
	}
	return &CommandResult{
		Reply:      "✅ System prompt updated.",
		SessionMod: &SessionModification{SystemPrompt: &raw},
		SkipAgent:  true,
	}, nil
}

func handleDebugCommand(ctx CommandContext) (*CommandResult, error) {
	cmd := ParseDebugCommand("/" + ctx.Command + " " + argRaw(ctx.Args))
	if cmd == nil {
		// Bare /debug defaults to show.
		cmd = &DebugCommand{Action: "show"}
	}

	switch cmd.Action {
	case "show":
		lines := []string{"🐛 Debug overrides (memory-only):"}
		if ctx.Session != nil {
			if ctx.Session.Model != "" {
				lines = append(lines, fmt.Sprintf("  model: %s", ctx.Session.Model))
			}
			if ctx.Session.Provider != "" {
				lines = append(lines, fmt.Sprintf("  provider: %s", ctx.Session.Provider))
			}
		}
		if len(lines) == 1 {
			lines = append(lines, "  (none)")
		}
		return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil

	case "reset":
		return &CommandResult{
			Reply:     "🐛 Debug overrides cleared.",
			SkipAgent: true,
		}, nil

	case "set":
		return &CommandResult{
			Reply:     fmt.Sprintf("🐛 Set debug `%s`.", cmd.Path),
			SkipAgent: true,
		}, nil

	case "unset":
		return &CommandResult{
			Reply:     fmt.Sprintf("🐛 Unset debug `%s`.", cmd.Path),
			SkipAgent: true,
		}, nil

	case "error":
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ %s", cmd.Message),
			SkipAgent: true,
			IsError:   true,
		}, nil

	default:
		return &CommandResult{
			Reply:     "Usage: /debug show|set|unset|reset",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
}
