// commands_handlers_session.go — Session lifecycle command handlers.
package handlers

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// --- Session lifecycle commands ---

func handleNewCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{
		Reply:      "🔄 New session started.",
		SessionMod: &types.SessionModification{Reset: true},
		SkipAgent:  true,
	}, nil
}

func handleResetCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{
		Reply:      "🔄 Session reset.",
		SessionMod: &types.SessionModification{Reset: true},
		SkipAgent:  true,
	}, nil
}

func handleForkCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session to fork.", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("🍴 Session forked from `%s`.", ctx.Session.SessionKey),
		SkipAgent: true,
	}, nil
}

func handleContinueCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /continue <session-id>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("▶️ Continuing session `%s`.", raw),
		SkipAgent: true,
	}, nil
}

func handleStopCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "⏹ Stopped.", SkipAgent: true}, nil
}

func handleCancelCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "❌ Cancelled.", SkipAgent: true}, nil
}

func handleKillCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "💀 Killed.", SkipAgent: true}, nil
}

func handleCompactCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	instructions := ""
	if raw != "" {
		instructions = raw
	}
	_ = instructions
	return &CommandResult{Reply: "📦 Context compacted.", SkipAgent: true}, nil
}

func handleExportCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session to export.", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("📄 Session `%s` exported.", ctx.Session.SessionKey),
		SkipAgent: true,
	}, nil
}

func handleSessionLifecycleCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{
			Reply:     "Usage: /session idle <duration|off> | /session max-age <duration|off>",
			SkipAgent: true,
		}, nil
	}

	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return &CommandResult{
			Reply:     "Usage: /session idle <duration|off> | /session max-age <duration|off>",
			SkipAgent: true, IsError: true,
		}, nil
	}

	action := strings.ToLower(parts[0])
	durationStr := parts[1]

	durationMs, err := parseSessionDuration(durationStr)
	if err != nil {
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Invalid duration: %s", err.Error()),
			SkipAgent: true, IsError: true,
		}, nil
	}

	switch action {
	case "idle":
		if durationMs == 0 {
			return &CommandResult{
				Reply:      "⏱ Session idle timeout disabled.",
				SessionMod: &types.SessionModification{IdleTimeoutMs: 0},
				SkipAgent:  true,
			}, nil
		}
		return &CommandResult{
			Reply:      fmt.Sprintf("⏱ Session idle timeout set to %s.", formatDurationHuman(durationMs)),
			SessionMod: &types.SessionModification{IdleTimeoutMs: durationMs},
			SkipAgent:  true,
		}, nil

	case "max-age":
		if durationMs == 0 {
			return &CommandResult{
				Reply:      "⏱ Session max age disabled.",
				SessionMod: &types.SessionModification{MaxAgeMs: 0},
				SkipAgent:  true,
			}, nil
		}
		return &CommandResult{
			Reply:      fmt.Sprintf("⏱ Session max age set to %s.", formatDurationHuman(durationMs)),
			SessionMod: &types.SessionModification{MaxAgeMs: durationMs},
			SkipAgent:  true,
		}, nil

	default:
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Unknown session action: %s", action),
			SkipAgent: true, IsError: true,
		}, nil
	}
}

func handleActivationCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ActivationMention
		if ctx.Session != nil && ctx.Session.GroupActivation != "" {
			current = ctx.Session.GroupActivation
		}
		return &CommandResult{
			Reply:     fmt.Sprintf("👥 Group activation: **%s**\nOptions: mention, always", current),
			SkipAgent: true,
		}, nil
	}
	mode, ok := types.NormalizeGroupActivation(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /activation mention|always", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("👥 Group activation: **%s**", mode),
		SessionMod: &types.SessionModification{GroupActivation: mode},
		SkipAgent:  true,
	}, nil
}

func handleSendPolicyCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := "on"
		if ctx.Session != nil && ctx.Session.SendPolicy != "" {
			current = ctx.Session.SendPolicy
		}
		return &CommandResult{Reply: fmt.Sprintf("📤 Send policy: **%s**\nOptions: on, off, inherit", current), SkipAgent: true}, nil
	}
	policy, ok := reply.NormalizeSendPolicy(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /send on|off|inherit", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("📤 Send policy: **%s**", policy),
		SessionMod: &types.SessionModification{SendPolicy: string(policy)},
		SkipAgent:  true,
	}, nil
}
