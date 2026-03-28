// get_reply.go — Main reply generation entrypoint and orchestration.
// Mirrors src/auto-reply/reply/get-reply.ts, get-reply-run.ts,
// get-reply-directives.ts, get-reply-directives-apply.ts,
// get-reply-directives-utils.ts, get-reply-inline-actions.ts.
package autoreply

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/commands"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/directives"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
)

// GetReplyFromConfig is the main entry point for generating a reply.
// It orchestrates the full pipeline: session init → directives → model selection
// → agent execution → reply normalization.
func GetReplyFromConfig(ctx context.Context, msg *types.MsgContext, opts types.GetReplyOptions, deps ReplyDeps) ([]types.ReplyPayload, error) {
	// 1. Initialize session state.
	session := InitSessionForReply(msg, deps)

	// 2. Parse inline directives from message body.
	parsedDirectives := directives.ParseInlineDirectives(msg.BodyForAgent, nil)
	cleanedBody := parsedDirectives.Cleaned

	// 3. Handle directive-only messages (no user text).
	if directives.IsDirectiveOnly(parsedDirectives) {
		result := ApplyDirectivesToSession(parsedDirectives, session, deps)
		if result != nil {
			return result, nil
		}
		return nil, nil
	}

	// 4. Apply directives to session state.
	ApplyDirectivesToSession(parsedDirectives, session, deps)

	// 5. Resolve model.
	resolvedModel := ResolveModelForReply(session, parsedDirectives, deps)

	// 6. Build typing controller.
	var typingCtrl *typing.TypingController
	if !opts.SuppressTyping && opts.OnReplyStart != nil {
		typingCtrl = typing.NewTypingController(typing.TypingControllerConfig{
			OnStart: opts.OnReplyStart,
			OnStop:  opts.OnTypingCleanup,
			Policy:  opts.TypingPolicy,
		})
		typingCtrl.Start()
		defer typingCtrl.Stop()
	}

	// 7. Run agent turn.
	agentCfg := AgentTurnConfig{
		SessionKey:     session.SessionKey,
		AgentID:        session.AgentID,
		Model:          resolvedModel.Model,
		Provider:       resolvedModel.Provider,
		Message:        cleanedBody,
		ThinkLevel:     session.ThinkLevel,
		FastMode:       session.FastMode,
		VerboseLevel:   session.VerboseLevel,
		ReasoningLevel: session.ReasoningLevel,
		ElevatedLevel:  session.ElevatedLevel,
	}

	result, err := deps.Agent.RunTurn(ctx, agentCfg)
	if err != nil {
		return nil, err
	}

	// 8. Normalize and filter payloads.
	normalized := reply.FilterReplyPayloads(result.Payloads, reply.NormalizeOpts{
		HeartbeatMode:        tokens.StripModeMessage,
		HeartbeatAckMaxChars: tokens.DefaultHeartbeatAckChars,
	})

	return normalized, nil
}

// ReplyDeps provides dependencies for reply generation.
type ReplyDeps struct {
	Agent       AgentExecutor
	Registry    *commands.CommandRegistry
	Router      *commands.CommandRouter
	History     *session.HistoryTracker
	SessionFunc func(key string) *types.SessionState // resolve session by key
}

// InitSessionForReply initializes or retrieves session state for a reply.
func InitSessionForReply(msg *types.MsgContext, deps ReplyDeps) *types.SessionState {
	if deps.SessionFunc != nil {
		existing := deps.SessionFunc(msg.SessionKey)
		if existing != nil {
			return existing
		}
	}
	return &types.SessionState{
		SessionKey: msg.SessionKey,
		Channel:    msg.Channel,
		AccountID:  msg.AccountID,
		ThreadID:   msg.ThreadID,
		IsGroup:    msg.IsGroup,
	}
}

// ApplyDirectivesToSession applies parsed directives to the session state.
// Returns reply payloads if the directive was handled inline (e.g., status query).
func ApplyDirectivesToSession(parsed directives.InlineDirectives, session *types.SessionState, deps ReplyDeps) []types.ReplyPayload {
	if parsed.HasThinkDirective {
		session.ThinkLevel = parsed.ThinkLevel
	}
	if parsed.HasVerboseDirective {
		session.VerboseLevel = parsed.VerboseLevel
	}
	if parsed.HasFastDirective {
		session.FastMode = parsed.FastMode
	}
	if parsed.HasReasoningDirective {
		session.ReasoningLevel = parsed.ReasoningLevel
	}
	if parsed.HasElevatedDirective {
		session.ElevatedLevel = parsed.ElevatedLevel
	}
	if parsed.HasModelDirective && parsed.RawModelDirective != "" {
		session.Model = parsed.RawModelDirective
	}

	// Handle /status as inline directive.
	if parsed.HasStatusDirective && deps.Router != nil {
		result, _ := deps.Router.Dispatch(commands.CommandContext{
			Command: "status",
			Session: session,
		})
		if result != nil && result.Reply != "" {
			return []types.ReplyPayload{{Text: result.Reply}}
		}
	}

	return nil
}

// ResolveModelForReply determines the model to use for a reply.
func ResolveModelForReply(session *types.SessionState, parsed directives.InlineDirectives, deps ReplyDeps) model.ModelSelection {
	modelName := session.Model
	provider := session.Provider

	if parsed.HasModelDirective && parsed.RawModelDirective != "" {
		// Parse provider/model from directive.
		parts := splitProviderModel(parsed.RawModelDirective)
		if parts[0] != "" {
			provider = parts[0]
		}
		if parts[1] != "" {
			modelName = parts[1]
		}
	}

	return model.ModelSelection{
		Provider:   provider,
		Model:      modelName,
		IsOverride: parsed.HasModelDirective,
	}
}

func splitProviderModel(ref string) [2]string {
	idx := -1
	for i, c := range ref {
		if c == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return [2]string{"", ref}
	}
	return [2]string{ref[:idx], ref[idx+1:]}
}
