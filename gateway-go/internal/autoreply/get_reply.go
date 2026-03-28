// get_reply.go — Main reply generation entrypoint and orchestration.
// Mirrors src/auto-reply/reply/get-reply.ts, get-reply-run.ts,
// get-reply-directives.ts, get-reply-directives-apply.ts,
// get-reply-directives-utils.ts, get-reply-inline-actions.ts.
package autoreply

import (
	"context"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// GetReplyFromConfig is the main entry point for generating a reply.
// It orchestrates the full pipeline: session init → directives → model selection
// → agent execution → reply normalization.
func GetReplyFromConfig(ctx context.Context, msg *types.MsgContext, opts types.GetReplyOptions, deps ReplyDeps) ([]types.ReplyPayload, error) {
	// 1. Initialize session state.
	session := InitSessionForReply(msg, deps)

	// 2. Parse inline directives from message body.
	directives := ParseInlineDirectives(msg.BodyForAgent, nil)
	cleanedBody := directives.Cleaned

	// 3. Handle directive-only messages (no user text).
	if IsDirectiveOnly(directives) {
		result := ApplyDirectivesToSession(directives, session, deps)
		if result != nil {
			return result, nil
		}
		return nil, nil
	}

	// 4. Apply directives to session state.
	ApplyDirectivesToSession(directives, session, deps)

	// 5. Resolve model.
	model := ResolveModelForReply(session, directives, deps)

	// 6. Build typing controller.
	var typingCtrl *TypingController
	if !opts.SuppressTyping && opts.OnReplyStart != nil {
		typingCtrl = NewTypingController(TypingControllerConfig{
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
		Model:          model.Model,
		Provider:       model.Provider,
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
	normalized := FilterReplyPayloads(result.Payloads, NormalizeOpts{
		HeartbeatMode:        tokens.StripModeMessage,
		HeartbeatAckMaxChars: tokens.DefaultHeartbeatAckChars,
	})

	return normalized, nil
}

// ReplyDeps provides dependencies for reply generation.
type ReplyDeps struct {
	Agent       AgentExecutor
	Registry    *CommandRegistry
	Router      *CommandRouter
	History     *HistoryTracker
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
func ApplyDirectivesToSession(directives InlineDirectives, session *types.SessionState, deps ReplyDeps) []types.ReplyPayload {
	if directives.HasThinkDirective {
		session.ThinkLevel = directives.ThinkLevel
	}
	if directives.HasVerboseDirective {
		session.VerboseLevel = directives.VerboseLevel
	}
	if directives.HasFastDirective {
		session.FastMode = directives.FastMode
	}
	if directives.HasReasoningDirective {
		session.ReasoningLevel = directives.ReasoningLevel
	}
	if directives.HasElevatedDirective {
		session.ElevatedLevel = directives.ElevatedLevel
	}
	if directives.HasModelDirective && directives.RawModelDirective != "" {
		session.Model = directives.RawModelDirective
	}

	// Handle /status as inline directive.
	if directives.HasStatusDirective && deps.Router != nil {
		result, _ := deps.Router.Dispatch(CommandContext{
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
func ResolveModelForReply(session *types.SessionState, directives InlineDirectives, deps ReplyDeps) ModelSelection {
	model := session.Model
	provider := session.Provider

	if directives.HasModelDirective && directives.RawModelDirective != "" {
		// Parse provider/model from directive.
		parts := pipeline.SplitProviderModel(directives.RawModelDirective)
		if parts[0] != "" {
			provider = parts[0]
		}
		if parts[1] != "" {
			model = parts[1]
		}
	}

	return ModelSelection{
		Provider:   provider,
		Model:      model,
		IsOverride: directives.HasModelDirective,
	}
}
