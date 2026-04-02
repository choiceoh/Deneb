// get_reply.go — Main reply generation entrypoint and orchestration.
// Mirrors src/auto-reply/reply/get-reply.ts, get-reply-run.ts,
// get-reply-directives.ts, get-reply-directives-apply.ts,
// get-reply-directives-utils.ts, get-reply-inline-actions.ts.
package autoreply

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/directives"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/thinking"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
)

// GetReplyFromConfig is the main entry point for generating a reply.
// It orchestrates the full pipeline: session init → preprocess hooks →
// directives (parse + handle) → model selection → agent execution →
// fallback tracking → reply normalization.
func GetReplyFromConfig(ctx context.Context, msg *types.MsgContext, opts types.GetReplyOptions, deps ReplyDeps) ([]types.ReplyPayload, error) {
	// 1. Initialize session state.
	sess := InitSessionForReply(msg, deps)

	// 2. Run preprocess hooks on the inbound message before directive parsing.
	if err := RunPreprocessHooks(msg, deps.PreprocessHooks); err != nil {
		return nil, err
	}

	// 3. Parse inline directives from message body.
	inline := directives.ParseInlineDirectives(msg.BodyForAgent, nil)

	// 4. Run full directive handling (model resolution, queue changes, ack text,
	//    session modifications) as a secondary processor after basic parsing.
	directiveResult := directives.HandleDirectives(msg.BodyForAgent, sess, directives.DirectiveHandlingOptions{
		ModelCandidates: deps.ModelCandidates,
		StatusHandler: func(session *types.SessionState) string {
			if deps.Router == nil {
				return ""
			}
			r, err := deps.Router.Dispatch(handlers.CommandContext{
				Command: "status",
				Session: session,
				Deps:    deps.CommandDeps,
			})
			if err != nil {
				slog.Warn("status directive dispatch failed", "error", err)
				return ""
			}
			if r != nil {
				return r.Reply
			}
			return ""
		},
	})

	// 5. Handle directive-only messages (no user text after directive removal).
	cleanedBody := directiveResult.CleanedBody
	if directiveResult.IsDirectiveOnly {
		// Persist directive changes to session.
		directives.PersistDirectives(sess, directiveResult)
		// Return immediate reply (e.g., ack text or status).
		if directiveResult.ImmediateReply != nil {
			return []types.ReplyPayload{*directiveResult.ImmediateReply}, nil
		}
		if directiveResult.AckText != "" {
			return []types.ReplyPayload{{Text: directiveResult.AckText}}, nil
		}
		return nil, nil
	}

	// 6. Apply parsed directive changes to session state.
	directives.PersistDirectives(sess, directiveResult)
	// Also apply basic directive fields that PersistDirectives may not cover.
	ApplyDirectivesToSession(inline, sess, deps)

	// 7. Resolve model using the full model selection pipeline.
	selection := ResolveModelForReply(sess, inline, deps)

	// 8. Resolve token limits from model runtime info.
	contextTokens := model.ResolveContextTokens(opts.ContextTokens, 0)
	maxTokens := model.ResolveMaxTokens(opts.MaxTokens, 0)

	// 8b. Resolve model-aware default thinking level if not set via directive.
	// The thinking package checks adaptive patterns (e.g., claude-4.6 → adaptive)
	// and catalog reasoning flags to pick an appropriate default.
	if sess.ThinkLevel == "" || sess.ThinkLevel == types.ThinkOff {
		if deps.ThinkingRuntime != nil {
			catalog := buildThinkingCatalog(deps.ModelCandidates)
			defaultThink := deps.ThinkingRuntime.ResolveThinkingDefaultForModel(
				selection.Provider, selection.Model, catalog,
			)
			if defaultThink != types.ThinkOff {
				sess.ThinkLevel = defaultThink
			}
		}
	}

	// 9. Build typing controller.
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

	// 10. Run agent turn.
	agentCfg := AgentTurnConfig{
		SessionKey:     sess.SessionKey,
		AgentID:        sess.AgentID,
		Model:          selection.Model,
		Provider:       selection.Provider,
		Message:        cleanedBody,
		ThinkLevel:     sess.ThinkLevel,
		FastMode:       sess.FastMode,
		VerboseLevel:   sess.VerboseLevel,
		ReasoningLevel: sess.ReasoningLevel,
		ElevatedLevel:  sess.ElevatedLevel,
		ContextTokens:  contextTokens,
		MaxTokens:      maxTokens,
		AuthProfile:    selection.AuthProfile,
		DeepWork:       sess.DeepWork,
	}

	// Record inbound user message in history tracker.
	if deps.History != nil && cleanedBody != "" {
		deps.History.Append(sess.SessionKey, session.HistoryEntry{
			Role: "user",
			Text: cleanedBody,
		})
	}

	result, err := deps.Agent.RunTurn(ctx, agentCfg)
	if err != nil {
		return nil, err
	}

	// Record assistant reply in history tracker.
	if deps.History != nil && result != nil && result.OutputText != "" {
		deps.History.Append(sess.SessionKey, session.HistoryEntry{
			Role: "assistant",
			Text: result.OutputText,
		})
	}

	// 10b. Record run accounting on the session.
	if result != nil {
		accounting := &session.SessionRunAccounting{}
		accounting.RecordRun(session.TokenUsage{
			InputTokens:  result.TokensUsed.InputTokens,
			OutputTokens: result.TokensUsed.OutputTokens,
			TotalTokens:  result.TokensUsed.TotalTokens,
		}, result.DurationMs)
	}

	// 11. Track model fallback transitions for user notification.
	if result != nil && result.FallbackActive {
		transition := model.ResolveFallbackTransition(
			selection.Provider, selection.Model,
			result.ProviderUsed, result.ModelUsed,
			result.FallbackAttempts, nil,
		)
		if transition.FallbackTransitioned {
			notice := model.BuildFallbackNotice(
				selection.Provider, selection.Model,
				result.ProviderUsed, result.ModelUsed,
				result.FallbackAttempts,
			)
			if notice != "" {
				result.Payloads = append([]types.ReplyPayload{{Text: notice}}, result.Payloads...)
			}
		}
		if transition.FallbackCleared {
			cleared := model.BuildFallbackClearedNotice(
				selection.Provider, selection.Model,
				transition.PreviousState.ActiveModel,
			)
			if cleared != "" {
				result.Payloads = append([]types.ReplyPayload{{Text: cleared}}, result.Payloads...)
			}
		}
	}

	// 12. Build deliverable reply payloads via the full reply pipeline:
	// heartbeat stripping, leaked tool-call removal, threading directives,
	// messaging tool dedup, and silent reply suppression.
	built := reply.BuildReplyPayloads(types.BuildReplyPayloadsParams{
		Payloads:         result.Payloads,
		IsHeartbeat:      opts.IsHeartbeat,
		CurrentMessageID: msg.MessageSid,
		OriginTo:         msg.To,
		AccountID:        msg.AccountID,
	})

	// 13. Final normalization pass (response prefix, heartbeat ack mode).
	normalized := reply.FilterReplyPayloads(built, reply.NormalizeOpts{
		HeartbeatMode:        tokens.StripModeMessage,
		HeartbeatAckMaxChars: tokens.DefaultHeartbeatAckChars,
	})

	return normalized, nil
}

// ReplyDeps provides dependencies for reply generation.
type ReplyDeps struct {
	Agent           AgentExecutor
	Registry        *handlers.CommandRegistry
	Router          *handlers.CommandRouter
	History         *session.HistoryTracker
	AbortMemory     *session.AbortMemory                   // tracks recently aborted sessions for dedup
	AbortCutoff     *session.SessionAbortCutoffEntry        // current abort cutoff marker for message filtering
	SessionFunc     func(key string) *types.SessionState   // resolve session by key
	PreprocessHooks []MessagePreprocessHook                 // hooks to run before directive parsing
	ModelCandidates []model.ModelCandidate                  // available models for directive resolution
	CommandDeps     *handlers.CommandDeps                   // server-level deps for command handlers (status, subagents, etc.)
	// OnSessionEvent fires session lifecycle hooks (abort, reset, etc.)
	// via the plugin hook system. nil = events silently dropped.
	OnSessionEvent func(eventType, sessionKey, reason string)
	// ThinkingRuntime resolves model-aware thinking defaults (adaptive, xhigh, etc.).
	// Optional; nil = no model-aware thinking defaults (ThinkOff unless /think directive).
	ThinkingRuntime *thinking.ThinkingRuntime
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
		SessionOrigin: msg.SessionOrigin,
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
	if inline.HasDeepWorkDirective {
		session.DeepWork = true
	}

	// Handle /status as inline directive.
	if inline.HasStatusDirective && deps.Router != nil {
		result, err := deps.Router.Dispatch(handlers.CommandContext{
			Command: "status",
			Session: session,
			Deps:    deps.CommandDeps,
		})
		if err != nil {
			slog.Warn("status directive dispatch failed", "error", err)
		}
		if result != nil && result.Reply != "" {
			return []types.ReplyPayload{{Text: result.Reply}}
		}
	}

	return nil
}

// buildThinkingCatalog converts model candidates to thinking catalog entries.
// ModelCandidate doesn't carry a Reasoning flag, so entries default to
// reasoning=false. The thinking package primarily uses regex patterns
// (from model_caps.yaml) rather than catalog reasoning flags for default resolution.
func buildThinkingCatalog(candidates []model.ModelCandidate) []thinking.ThinkingCatalogEntry {
	entries := make([]thinking.ThinkingCatalogEntry, len(candidates))
	for i, c := range candidates {
		entries[i] = thinking.ThinkingCatalogEntry{
			Provider: c.Provider,
			ID:       c.Model,
		}
	}
	return entries
}

// ResolveModelForReply determines the model to use for a reply via the full
// model selection pipeline: directive > session > config > default.
func ResolveModelForReply(sess *types.SessionState, inline directives.InlineDirectives, deps ReplyDeps) model.ModelSelection {
	// Parse directive provider/model if present.
	var directiveProvider, directiveModel, directiveProfile string
	if inline.HasModelDirective && inline.RawModelDirective != "" {
		parts := pipeline.SplitProviderModel(inline.RawModelDirective)
		directiveProvider = parts[0]
		directiveModel = parts[1]
		directiveProfile = inline.RawModelProfile
	}

	// Run the full model resolution pipeline.
	state := model.ResolveModelSelection(model.ModelSelectionConfig{
		DirectiveProvider: directiveProvider,
		DirectiveModel:    directiveModel,
		DirectiveProfile:  directiveProfile,
		SessionModel:      sess.Model,
		SessionProvider:   sess.Provider,
		Candidates:        deps.ModelCandidates,
	})

	return model.ModelSelection{
		Provider:    state.Provider,
		Model:       state.Model,
		IsOverride:  state.IsOverride,
		IsFallback:  state.IsFallback,
		AuthProfile: state.AuthProfile,
	}
}
