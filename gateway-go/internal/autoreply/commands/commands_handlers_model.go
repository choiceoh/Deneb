// commands_handlers_model.go — Model, thinking, and inference mode command handlers.
package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func handleModelCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		if ctx.Session != nil && ctx.Session.Model != "" {
			return &CommandResult{
				Reply:     fmt.Sprintf("🤖 Current model: %s", model.FormatProviderModelRef(ctx.Session.Provider, ctx.Session.Model)),
				SkipAgent: true,
			}, nil
		}
		return &CommandResult{Reply: "Usage: /model <provider/model>", SkipAgent: true}, nil
	}

	// Try to resolve the model from candidates.
	var candidates []model.ModelCandidate
	if ctx.Deps != nil {
		candidates = ctx.Deps.ModelCandidates
	}
	resolved := model.ResolveModelFromDirective(raw, candidates)

	provider := ""
	modelStr := raw
	if resolved != nil {
		provider = resolved.Provider
		modelStr = resolved.Model
	} else {
		parts := splitProviderModel(raw)
		provider = parts[0]
		modelStr = parts[1]
	}

	return &CommandResult{
		Reply:      fmt.Sprintf("🤖 Model set to: %s", model.FormatProviderModelRef(provider, modelStr)),
		SessionMod: &types.SessionModification{Model: modelStr, Provider: provider},
		SkipAgent:  true,
	}, nil
}

func handleModelsListCommand(ctx CommandContext) (*CommandResult, error) {
	var candidates []model.ModelCandidate
	if ctx.Deps != nil {
		candidates = ctx.Deps.ModelCandidates
	}
	if len(candidates) == 0 {
		return &CommandResult{Reply: "No models available.", SkipAgent: true}, nil
	}

	// Parse pagination args.
	raw := argRaw(ctx.Args)
	page := 0
	limit := 15
	if raw != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && p > 0 {
			page = p - 1
		}
	}

	start := page * limit
	if start >= len(candidates) {
		return &CommandResult{Reply: "No more models.", SkipAgent: true}, nil
	}
	end := start + limit
	if end > len(candidates) {
		end = len(candidates)
	}

	var lines []string
	lines = append(lines, "📋 **Available Models:**\n")
	for _, c := range candidates[start:end] {
		ref := model.FormatProviderModelRef(c.Provider, c.Model)
		label := c.Label
		if label == "" {
			label = c.Model
		}
		lines = append(lines, fmt.Sprintf("• `%s` — %s", ref, label))
	}
	if end < len(candidates) {
		lines = append(lines, fmt.Sprintf("\n_Page %d. Use /models %d for next._", page+1, page+2))
	}

	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleThinkCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ThinkOff
		if ctx.Session != nil && ctx.Session.ThinkLevel != "" {
			current = ctx.Session.ThinkLevel
		}
		labels := types.FormatThinkingLevels("", ", ")
		return &CommandResult{
			Reply:     fmt.Sprintf("🧠 Thinking: **%s**\nOptions: %s", current, labels),
			SkipAgent: true,
		}, nil
	}
	level, ok := types.NormalizeThinkLevel(raw)
	if !ok {
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Unknown thinking level: `%s`\nOptions: %s", raw, types.FormatThinkingLevels("", ", ")),
			SkipAgent: true, IsError: true,
		}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("🧠 Thinking set to: **%s**", level),
		SessionMod: &types.SessionModification{ThinkLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleFastCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" || raw == "status" {
		mode := "off"
		if ctx.Session != nil && ctx.Session.FastMode {
			mode = "on"
		}
		return &CommandResult{Reply: fmt.Sprintf("⚡ Fast mode: **%s**", mode), SkipAgent: true}, nil
	}
	val, ok := types.NormalizeFastMode(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /fast on|off|status", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("⚡ Fast mode: **%s**", boolToOnOff(val)),
		SessionMod: &types.SessionModification{FastMode: &val},
		SkipAgent:  true,
	}, nil
}

func handleVerboseCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.VerboseOff
		if ctx.Session != nil && ctx.Session.VerboseLevel != "" {
			current = ctx.Session.VerboseLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("📝 Verbose: **%s**\nOptions: off, on, full", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeVerboseLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /verbose off|on|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("📝 Verbose: **%s**", level),
		SessionMod: &types.SessionModification{VerboseLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleReasoningCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ReasoningOff
		if ctx.Session != nil && ctx.Session.ReasoningLevel != "" {
			current = ctx.Session.ReasoningLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("💭 Reasoning: **%s**\nOptions: off, on, stream", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeReasoningLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /reasoning off|on|stream", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("💭 Reasoning: **%s**", level),
		SessionMod: &types.SessionModification{ReasoningLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleElevatedCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ElevatedOff
		if ctx.Session != nil && ctx.Session.ElevatedLevel != "" {
			current = ctx.Session.ElevatedLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("🔓 Elevated: **%s**\nOptions: off, on, ask, full", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeElevatedLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /elevated off|on|ask|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("🔓 Elevated: **%s**", level),
		SessionMod: &types.SessionModification{ElevatedLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleUsageCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "📊 Usage display options: off, tokens, full", SkipAgent: true}, nil
	}
	level, ok := types.NormalizeUsageDisplay(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /usage off|tokens|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("📊 Usage display: **%s**", level), SkipAgent: true}, nil
}

// splitProviderModel splits a "provider/model" reference into [provider, model].
// If no slash is present, returns ["", ref].
func splitProviderModel(ref string) [2]string {
	if idx := strings.Index(ref, "/"); idx >= 0 {
		return [2]string{ref[:idx], ref[idx+1:]}
	}
	return [2]string{"", ref}
}
