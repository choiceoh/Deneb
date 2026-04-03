// commands_handlers_model.go — Model and verbose command handlers.
package handlers

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
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

	resolved := model.ResolveModelFromDirective(raw, nil)

	provider := ""
	modelStr := raw
	if resolved != nil {
		provider = resolved.Provider
		modelStr = resolved.Model
	} else {
		parts := pipeline.SplitProviderModel(raw)
		provider = parts[0]
		modelStr = parts[1]
	}

	return &CommandResult{
		Reply:      fmt.Sprintf("🤖 Model set to: %s", model.FormatProviderModelRef(provider, modelStr)),
		SessionMod: &types.SessionModification{Model: modelStr, Provider: provider},
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
