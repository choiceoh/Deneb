// get_reply.go — Main reply generation entrypoint and orchestration.
// Mirrors src/auto-reply/reply/get-reply.ts, get-reply-run.ts,
// get-reply-directives.ts, get-reply-directives-apply.ts,
// get-reply-directives-utils.ts, get-reply-inline-actions.ts.
package autoreply

import (
	"context"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/directives"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
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
	inline := directives.ParseInlineDirectives(msg.BodyForAgent, nil)
	cleanedBody := inline.Cleaned

	// 3. Handle directive-only messages (no user text).
	if directives.IsDirectiveOnly(inline) {
		result := ApplyDirectivesToSession(inline, session, deps)
		if result != nil {
			return result, nil
		}
		return nil, nil
	}

	// 4. Apply directives to session state.
	ApplyDirectivesToSession(inline, session, deps)

	// 5. Resolve model.
	selection := ResolveModelForReply(session, inline, deps)

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
		Model:          selection.Model,
		Provider:       selection.Provider,
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
	Registry    *handlers.CommandRegistry
	Router      *handlers.CommandRouter
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
func ApplyDirectivesToSession(inline directives.InlineDirectives, session *types.SessionState, deps ReplyDeps) []types.ReplyPayload {
	if inline.HasThinkDirective {
		session.ThinkLevel = inline.ThinkLevel
	}
	if inline.HasVerboseDirective {
		session.VerboseLevel = inline.VerboseLevel
	}
	if inline.HasFastDirective {
		session.FastMode = inline.FastMode
	}
	if inline.HasReasoningDirective {
		session.ReasoningLevel = inline.ReasoningLevel
	}
	if inline.HasElevatedDirective {
		session.ElevatedLevel = inline.ElevatedLevel
	}
	if inline.HasModelDirective && inline.RawModelDirective != "" {
		session.Model = inline.RawModelDirective
	}

	// Handle /status as inline directive.
	if inline.HasStatusDirective && deps.Router != nil {
		result, _ := deps.Router.Dispatch(handlers.CommandContext{
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
func ResolveModelForReply(session *types.SessionState, inline directives.InlineDirectives, deps ReplyDeps) model.ModelSelection {
	modelName := session.Model
	provider := session.Provider

	if inline.HasModelDirective && inline.RawModelDirective != "" {
		// Parse provider/model from directive.
		parts := pipeline.SplitProviderModel(inline.RawModelDirective)
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
		IsOverride: inline.HasModelDirective,
	}
}
