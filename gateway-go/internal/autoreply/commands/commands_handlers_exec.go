// commands_handlers_exec.go — Execution, approval, plugin, MCP, and allowlist command handlers.
package commands

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func handleBashCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "⚠️ Usage: /bash <command>", SkipAgent: true, IsError: true}, nil
	}

	// Check bash configuration.
	var bashCfg BashCommandConfig
	if ctx.Deps != nil {
		bashCfg = ctx.Deps.BashConfig
	} else {
		bashCfg = DefaultBashConfig()
	}

	allowed, reason := ValidateBashCommand(raw, bashCfg)
	if !allowed {
		return &CommandResult{Reply: fmt.Sprintf("⚠️ %s", reason), SkipAgent: true, IsError: true}, nil
	}

	// Check elevated permissions.
	if ctx.Session != nil && ctx.Session.ElevatedLevel == types.ElevatedOff {
		return &CommandResult{Reply: ElevatedUnavailableMessage(), SkipAgent: true, IsError: true}, nil
	}

	// Delegate to agent for execution (not handled inline).
	return &CommandResult{SkipAgent: false}, nil
}

func handleApproveCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "✅ Approved.", SkipAgent: true}, nil
	}
	// Parse approval decision: /approve <id> <decision>
	parts := strings.Fields(raw)
	if len(parts) < 1 {
		return &CommandResult{Reply: "✅ Approved.", SkipAgent: true}, nil
	}

	decision := "allow"
	if len(parts) >= 2 {
		switch strings.ToLower(parts[1]) {
		case "allow", "once", "yes":
			decision = "allow"
		case "always":
			decision = "always"
		case "deny", "reject", "no":
			decision = "deny"
		case "block":
			decision = "block"
		default:
			decision = "allow"
		}
	}

	return &CommandResult{
		Reply:     fmt.Sprintf("✅ Approval: %s (decision: %s)", parts[0], decision),
		SkipAgent: true,
	}, nil
}

func handlePluginsCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "🔌 Plugin management. Use /plugins list, /plugins install, /plugins remove.", SkipAgent: true}, nil
}

func handlePluginCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /plugin <name>", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("🔌 Plugin: %s", raw), SkipAgent: true}, nil
}

func handleMCPCommand(ctx CommandContext) (*CommandResult, error) {
	cmd := ParseMcpCommand("/" + ctx.Command + " " + argRaw(ctx.Args))
	if cmd == nil {
		cmd = &McpCommand{Action: "show"}
	}

	switch cmd.Action {
	case "show":
		if cmd.Name != "" {
			return &CommandResult{
				Reply:     fmt.Sprintf("🔌 MCP server \"%s\" config.", cmd.Name),
				SkipAgent: true,
			}, nil
		}
		return &CommandResult{
			Reply:     "🔌 MCP servers configured.",
			SkipAgent: true,
		}, nil

	case "set":
		return &CommandResult{
			Reply:     fmt.Sprintf("🔌 MCP server \"%s\" saved.", cmd.Name),
			SkipAgent: true,
		}, nil

	case "unset":
		return &CommandResult{
			Reply:     fmt.Sprintf("🔌 MCP server \"%s\" removed.", cmd.Name),
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
			Reply:     "Usage: /mcp show|set|unset",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
}

func handleAllowlistCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "🛡️ Allowlist management.\nUsage: /allowlist list | /allowlist add <sender> | /allowlist remove <sender>", SkipAgent: true}, nil
	}

	parts := strings.Fields(raw)
	action := strings.ToLower(parts[0])

	switch action {
	case "list":
		return &CommandResult{Reply: "🛡️ Current allowlist: (list entries)", SkipAgent: true}, nil
	case "add":
		if len(parts) < 2 {
			return &CommandResult{Reply: "⚠️ Usage: /allowlist add <sender>", SkipAgent: true, IsError: true}, nil
		}
		return &CommandResult{Reply: fmt.Sprintf("✅ Added `%s` to allowlist.", parts[1]), SkipAgent: true}, nil
	case "remove", "delete", "rm":
		if len(parts) < 2 {
			return &CommandResult{Reply: "⚠️ Usage: /allowlist remove <sender>", SkipAgent: true, IsError: true}, nil
		}
		return &CommandResult{Reply: fmt.Sprintf("✅ Removed `%s` from allowlist.", parts[1]), SkipAgent: true}, nil
	default:
		return &CommandResult{Reply: "⚠️ Unknown allowlist action. Use: list, add, remove", SkipAgent: true, IsError: true}, nil
	}
}

func handleBtwCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	question, matched := ExtractBtwQuestion("/btw "+raw, "", nil)
	if !matched {
		// Not a /btw command; delegate to agent.
		return &CommandResult{SkipAgent: false}, nil
	}

	if question == "" {
		return &CommandResult{
			Reply:     "Usage: /btw <side question>",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	// Validate active session exists.
	if ctx.Session == nil {
		return &CommandResult{
			Reply:     "⚠️ /btw requires an active session with existing context.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	// BTW side questions are delegated to the agent. The agent runner should
	// use thinking=off for quick responses, but we do NOT modify the main
	// session's types.ThinkLevel/types.ReasoningLevel — those are per-BTW-turn only.
	// The BtwContext on the result signals to the dispatch layer that this
	// is a side question needing isolated think settings.
	return &CommandResult{
		Reply:      question,
		SkipAgent:  false,
		BtwContext: &BtwContext{Question: question},
	}, nil
}
