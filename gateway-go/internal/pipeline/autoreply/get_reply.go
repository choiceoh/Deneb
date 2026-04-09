// get_reply.go — Main reply generation entrypoint and orchestration.
//
// Pipeline: session init → directive handling → model selection →
// thinking defaults → agent execution → fallback tracking → reply normalization.
package autoreply

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/directives"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/thinking"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/typing"
)

// ReplyFromConfig is the main entry point for generating a reply.
// Pipeline: session init → directive handling → model selection →
// thinking defaults → agent execution → reply normalization.
func ReplyFromConfig(ctx context.Context, msg *types.MsgContext, opts types.GetReplyOptions, deps ReplyDeps) ([]types.ReplyPayload, error) {
	// 1. Initialize session state.
	sess := initSessionForReply(msg, deps)

	// 2. Parse and handle directives (single pass — HandleDirectives calls
	//    ParseInlineDirectives internally).
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

	// 3. Handle directive-only messages (no user text after directive removal).
	cleanedBody := directiveResult.CleanedBody
	if directiveResult.IsDirectiveOnly {
		directives.PersistDirectives(sess, directiveResult)
		if directiveResult.ImmediateReply != nil {
			return []types.ReplyPayload{*directiveResult.ImmediateReply}, nil
		}
		if directiveResult.AckText != "" {
			return []types.ReplyPayload{{Text: directiveResult.AckText}}, nil
		}
		return nil, nil
	}

	// 4. Apply directive changes to session state.
	directives.PersistDirectives(sess, directiveResult)

	// 5. Resolve model (directive resolution > session > config > default).
	selection := resolveModelForReply(sess, directiveResult, deps)

	// 6. Resolve token limits.
	contextTokens := model.ResolveContextTokens(opts.ContextTokens, 0)
	maxTokens := model.ResolveMaxTokens(opts.MaxTokens, 0)

	// 7. Resolve model-aware thinking default if not set via directive.
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

	// 8. Build typing controller (if callbacks provided).
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

	// 9. Run agent turn.
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

	// Reply delivery happens asynchronously via chatSendExecutor →
	// chat.Handler.Send(). The returned Payloads are only used for
	// command-level replies (not agent-generated content).
	return result.Payloads, nil
}

// ReplyDeps provides dependencies for reply generation.
type ReplyDeps struct {
	Agent           AgentExecutor
	Registry        *handlers.CommandRegistry
	Router          *handlers.CommandRouter
	History         *session.HistoryTracker
	AbortMemory     *session.AbortMemory
	AbortCutoff     *session.SessionAbortCutoffEntry
	SessionFunc     func(key string) *types.SessionState
	ModelCandidates []model.ModelCandidate
	CommandDeps     *handlers.CommandDeps
	OnSessionEvent  func(eventType, sessionKey, reason string)
	ThinkingRuntime *thinking.ThinkingRuntime
}

// initSessionForReply initializes or retrieves session state for a reply.
func initSessionForReply(msg *types.MsgContext, deps ReplyDeps) *types.SessionState {
	if deps.SessionFunc != nil {
		if existing := deps.SessionFunc(msg.SessionKey); existing != nil {
			return existing
		}
	}
	return &types.SessionState{
		SessionOrigin: msg.SessionOrigin,
	}
}

// resolveModelForReply determines the model to use for a reply.
// Uses directive resolution from HandleDirectives if available, otherwise
// falls back to session/config defaults.
func resolveModelForReply(sess *types.SessionState, dr directives.DirectiveHandlingResult, deps ReplyDeps) model.ModelSelection {
	var directiveProvider, directiveModel, directiveProfile string
	if dr.ModelResolution != nil && dr.ModelResolution.IsValid {
		directiveProvider = dr.ModelResolution.Provider
		directiveModel = dr.ModelResolution.Model
		directiveProfile = dr.ModelResolution.AuthProfile
	}

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

// buildThinkingCatalog converts model candidates to thinking catalog entries.
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
