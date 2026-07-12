// Package session provides RPC handlers for session management methods.
//
// Methods: sessions.patch, sessions.reset, sessions.overflow_check, sessions.send,
// sessions.steer, sessions.abort, agent, agent.wait.
package session

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TranscriptDeleter removes a session's persisted transcript file.
type TranscriptDeleter interface {
	Delete(sessionKey string) error
}

// Deps holds dependencies for session RPC methods.
type Deps struct {
	Sessions    *session.Manager
	GatewaySubs *events.GatewayEventSubscriptions
	// Transcripts lazily resolves the transcript store (created after the
	// early registration phase). Optional, but without it sessions.delete
	// leaves the .jsonl behind and the startup restore resurrects the
	// session — the zombie bug miniapp.sessions.delete already fixed.
	Transcripts func() (TranscriptDeleter, error)
}

// ExecDeps holds dependencies for native session execution and agent RPC methods.
type ExecDeps struct {
	Chat       ExecChat
	JobTracker *agent.JobTracker
}

// ExecChat is the protocol-level session surface implemented by chat.Handler.
// Keeping it consumer-owned prevents the RPC handler from importing the chat
// pipeline implementation.
type ExecChat interface {
	ChatReady() bool
	SessionsSend(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
	SessionsSteer(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
	SessionsAbort(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
}

// Methods returns all session management RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"sessions.patch":          sessionsPatch(deps),
		"sessions.reset":          sessionsReset(deps),
		"sessions.overflow_check": sessionsOverflowCheck(deps),
	}
}

// ExecMethods returns session execution and agent RPC handlers.
// Returns nil if Chat is not available.
func ExecMethods(deps ExecDeps) map[string]rpcutil.HandlerFunc {
	if deps.Chat == nil || !deps.Chat.ChatReady() {
		return nil
	}

	return map[string]rpcutil.HandlerFunc{
		"sessions.send": func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return deps.Chat.SessionsSend(ctx, req)
		},
		"sessions.steer": func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return deps.Chat.SessionsSteer(ctx, req)
		},
		"sessions.abort": func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return deps.Chat.SessionsAbort(ctx, req)
		},
		"agent": func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return deps.Chat.SessionsSend(ctx, req)
		},
		"agent.wait": agentWait(deps),
	}
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

func sessionsPatch(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Key string `json:"key"`
		session.PatchFields
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			return nil, rpcerr.MissingParam("key")
		}

		updated := deps.Sessions.Patch(key, p.PatchFields)
		emitSessionLifecycle(deps.GatewaySubs, key, "patch")
		return map[string]any{
			"ok":    true,
			"key":   key,
			"entry": updated,
		}, nil
	})
}

// ---------------------------------------------------------------------------
// sessions.reset
// ---------------------------------------------------------------------------

func sessionsReset(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			return nil, rpcerr.MissingParam("key")
		}
		reason := "reset"
		if p.Reason == "new" {
			reason = "new"
		}

		s := deps.Sessions.ResetSession(key)
		if s == nil {
			return nil, rpcerr.NotFound("session").
				WithSession(rpcutil.TruncateForError(key))
		}

		emitSessionLifecycle(deps.GatewaySubs, key, reason)
		return map[string]any{
			"ok":    true,
			"key":   key,
			"entry": s,
		}, nil
	})
}

// ---------------------------------------------------------------------------
// sessions.overflow_check — checks context overflow state
// ---------------------------------------------------------------------------

func sessionsOverflowCheck(_ Deps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey    string `json:"sessionKey"`
		CurrentTokens int64  `json:"currentTokens"`
		MaxTokens     int64  `json:"maxTokens"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.MaxTokens <= 0 {
			return map[string]any{
				"isOverflow": false,
				"usage":      0.0,
			}, nil
		}

		currentTokens := p.CurrentTokens
		if currentTokens < 0 {
			// Treat malformed negative counters as empty usage. Allowing a
			// negative numerator through the prune formula yields a misleading
			// 50% emergency-prune recommendation.
			currentTokens = 0
		}
		usage := float64(currentTokens) / float64(p.MaxTokens)
		isOverflow := usage > 0.9 // 90% threshold

		return map[string]any{
			"isOverflow":          isOverflow,
			"usage":               usage,
			"emergencyPruneRatio": minf(maxf((usage-0.7)/usage, 0), 0.5),
		}, nil
	})
}

// ---------------------------------------------------------------------------
// agent.wait
// ---------------------------------------------------------------------------

func agentWait(deps ExecDeps) rpcutil.HandlerFunc {
	type params struct {
		RunID        string `json:"runId"`
		TimeoutMs    int64  `json:"timeoutMs,omitempty"`
		IgnoreCached bool   `json:"ignoreCached,omitempty"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.RunID == "" {
			return nil, rpcerr.MissingParam("runId").
				WithMethod("agent.wait")
		}
		if deps.JobTracker == nil {
			return nil, rpcerr.Unavailable("job tracker not available").
				WithMethod("agent.wait")
		}
		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 60_000
		}
		snapshot := deps.JobTracker.WaitForJob(ctx, p.RunID, p.TimeoutMs, p.IgnoreCached)
		if snapshot == nil {
			return map[string]any{
				"status":  "timeout",
				"message": "job did not complete within timeout",
			}, nil
		}
		return snapshot, nil
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// emitSessionLifecycle emits a lifecycle change event if subs is available.
func emitSessionLifecycle(subs *events.GatewayEventSubscriptions, sessionKey, reason string) {
	if subs != nil {
		subs.EmitLifecycle(events.LifecycleChangeEvent{
			SessionKey: sessionKey,
			Reason:     reason,
		})
	}
}

// minf returns the smaller of two float64 values.
func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// maxf returns the larger of two float64 values.
func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
