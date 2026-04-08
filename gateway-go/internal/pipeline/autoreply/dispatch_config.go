// dispatch_config.go — Full dispatch orchestration from config.
// Mirrors src/auto-reply/reply/dispatch-from-config.ts (664 LOC),
// dispatch-acp.ts (367 LOC), dispatch-acp-delivery.ts (189 LOC),
// followup-runner.ts (415 LOC), origin-routing.ts (29 LOC),
// dispatcher-registry.ts (58 LOC), provider-dispatcher.ts (44 LOC).
package autoreply

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// DispatchConfig holds the resolved configuration for message dispatch.
type DispatchConfig struct {
	SessionKey   string
	AgentID      string
	Channel      string
	To           string
	AccountID    string
	ThreadID     string
	Model        string
	Provider     string
	IsGroup      bool
	IsHeartbeat  bool
	ReplyOptions types.GetReplyOptions
}

// DispatchResult holds the outcome of a full dispatch cycle.
type DispatchResult struct {
	Payloads []types.ReplyPayload
	Handled  bool
	Error    error
}

// DispatchFromConfig runs the full reply dispatch pipeline from config.
func DispatchFromConfig(ctx context.Context, msg *types.MsgContext, cfg DispatchConfig, deps ReplyDeps) DispatchResult {
	// 1. Check for abort trigger.
	if session.IsAbortRequestText(msg.Body) {
		// Record abort in memory so subsequent messages within the cooldown
		// window are not re-dispatched to the agent.
		if deps.AbortMemory != nil {
			deps.AbortMemory.Record(cfg.SessionKey, time.Now().UnixMilli())
		}
		if deps.OnSessionEvent != nil {
			deps.OnSessionEvent("abort", cfg.SessionKey, "user abort trigger")
		}
		return DispatchResult{Handled: true}
	}

	// 1b. Skip messages to recently aborted sessions (3-second cooldown).
	if deps.AbortMemory != nil && deps.AbortMemory.WasRecentlyAborted(cfg.SessionKey, 3000) {
		return DispatchResult{Handled: true}
	}

	// 1c. Skip messages that fall before the abort cutoff marker.
	if deps.AbortCutoff != nil {
		cutoff := session.ReadAbortCutoffFromSessionEntry(deps.AbortCutoff)
		if cutoff != nil {
			if session.ShouldSkipMessageByAbortCutoff(cutoff.MessageSid, cutoff.Timestamp, msg.MessageSid, nil) {
				return DispatchResult{Handled: true}
			}
		}
	}

	// 2. Check for command.
	if deps.Registry != nil && deps.Router != nil {
		normalized := deps.Registry.NormalizeCommandBody(msg.Body, "")
		if deps.Registry.HasControlCommand(normalized, "") {
			cmdKey := extractCommandKey(normalized)
			// Use ParseCommandArgs for proper positional argument parsing
			// when the command definition has typed args.
			args := extractCommandArgs(normalized, cmdKey)
			if args != nil {
				if cmd := deps.Registry.FindCommandByNativeName(cmdKey); cmd != nil {
					parsed := handlers.ParseCommandArgs(cmd, args.Raw)
					if parsed != nil {
						args = parsed
					}
				}
			}
			result, err := deps.Router.Dispatch(handlers.CommandContext{
				Command:    cmdKey,
				Args:       args,
				Body:       msg.Body,
				SessionKey: cfg.SessionKey,
				Channel:    cfg.Channel,
				IsGroup:    cfg.IsGroup,
				Msg:        msg,
				Deps:       deps.CommandDeps,
			})
			if err == nil && result != nil && result.SkipAgent {
				var payloads []types.ReplyPayload
				if result.Reply != "" {
					payloads = append(payloads, types.ReplyPayload{Text: result.Reply, IsError: result.IsError})
				}
				payloads = append(payloads, result.Payloads...)
				return DispatchResult{Payloads: payloads, Handled: true}
			}
		}
	}

	// 2b. Check for inline command tokens (e.g., "!model gpt-4" embedded in text).
	// These indicate the message body contains inline commands that should be
	// processed as directives during agent reply generation.
	if handlers.HasInlineCommandTokens(msg.Body) {
		msg.CommandSource = "inline"
	}

	// 3. Generate reply via agent.
	payloads, err := ReplyFromConfig(ctx, msg, cfg.ReplyOptions, deps)
	if err != nil {
		return DispatchResult{Error: err}
	}

	return DispatchResult{Payloads: payloads, Handled: true}
}

func extractCommandArgs(normalized, cmdKey string) *handlers.CommandArgs {
	prefix := "/" + cmdKey
	if len(normalized) <= len(prefix) {
		return nil
	}
	rest := normalized[len(prefix):]
	if rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		raw := rest[1:]
		return &handlers.CommandArgs{Raw: raw}
	}
	return nil
}

// OriginRouting determines the reply target based on message origin.
type OriginRouting struct {
	Channel   string
	To        string
	AccountID string
	ThreadID  string
}

// ResolveOriginRouting extracts routing info from the inbound message.
func ResolveOriginRouting(msg *types.MsgContext) OriginRouting {
	return OriginRouting{
		Channel:   msg.Channel,
		To:        msg.To,
		AccountID: msg.AccountID,
		ThreadID:  msg.ThreadID,
	}
}
