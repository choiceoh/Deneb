// commands_handlers_info.go — Status, info, and help command handlers.
package autoreply

import (
	"fmt"
	"strconv"
	"strings"
)

func handleStatusCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	s := ctx.Session
	report := StatusReport{
		SessionKey:      s.SessionKey,
		AgentID:         s.AgentID,
		Model:           s.Model,
		Provider:        s.Provider,
		Channel:         s.Channel,
		IsGroup:         s.IsGroup,
		ThinkLevel:      s.ThinkLevel,
		FastMode:        s.FastMode,
		VerboseLevel:    s.VerboseLevel,
		ReasoningLevel:  s.ReasoningLevel,
		ElevatedLevel:   s.ElevatedLevel,
		SendPolicy:      s.SendPolicy,
		GroupActivation: s.GroupActivation,
	}
	// Populate server-level fields from StatusDeps.
	if ctx.Deps != nil && ctx.Deps.Status != nil {
		sd := ctx.Deps.Status
		report.Version = sd.Version
		report.StartedAt = sd.StartedAt
		report.RustFFI = sd.RustFFI
		report.SessionCount = sd.SessionCount
		report.WSConnections = sd.WSConnections
		report.ProviderUsage = sd.ProviderUsage
		report.ChannelHealth = sd.ChannelHealth
	}
	return &CommandResult{Reply: BuildStatusMessage(report), SkipAgent: true}, nil
}

func handleHelpCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	page := 0
	if raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}
	commands := BuiltinChatCommands()
	if page > 0 {
		return &CommandResult{Reply: BuildCommandsMessage(commands, page, 20), SkipAgent: true}, nil
	}
	return &CommandResult{Reply: BuildHelpMessage(commands), SkipAgent: true}, nil
}

func handleContextCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	return &CommandResult{
		Reply:     "📊 Context usage report generated.",
		SkipAgent: true,
	}, nil
}

func handleInfoCommand(ctx CommandContext) (*CommandResult, error) {
	var lines []string
	lines = append(lines, "ℹ️ **Agent Info**")
	if ctx.Session != nil {
		lines = append(lines, fmt.Sprintf("Session: `%s`", ctx.Session.SessionKey))
		lines = append(lines, fmt.Sprintf("Agent: `%s`", ctx.Session.AgentID))
		lines = append(lines, fmt.Sprintf("Channel: %s", ctx.Session.Channel))
		if ctx.Session.Model != "" {
			lines = append(lines, fmt.Sprintf("Model: %s", FormatProviderModelRef(ctx.Session.Provider, ctx.Session.Model)))
		}
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}
