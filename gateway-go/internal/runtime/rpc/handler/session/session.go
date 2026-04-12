// Package session provides RPC handlers for session management methods.
//
// Methods: sessions.patch, sessions.reset, sessions.overflow_check, sessions.send,
// sessions.steer, sessions.abort, agent, agent.wait.
package session

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for session RPC methods.
type Deps struct {
	Sessions    *session.Manager
	GatewaySubs *events.GatewayEventSubscriptions
}

// ExecDeps holds dependencies for native session execution and agent RPC methods.
type ExecDeps struct {
	Chat       *chat.Handler
	JobTracker *agent.JobTracker
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
	if deps.Chat == nil {
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

		usage := float64(p.CurrentTokens) / float64(p.MaxTokens)
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

// normalizeKeys trims whitespace from keys, drops empty entries, and caps at max.
func normalizeKeys(raw []string, maxKeys int) []string {
	keys := make([]string, 0, len(raw))
	for _, k := range raw {
		if trimmed := strings.TrimSpace(k); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	return keys
}

// isTrue returns true only if the pointer is non-nil and points to true.
func isTrue(p *bool) bool {
	return p != nil && *p
}

// boolCount returns how many of the given booleans are true.
func boolCount(vals ...bool) int {
	n := 0
	for _, v := range vals {
		if v {
			n++
		}
	}
	return n
}

// filterSessions applies optional filters to a session list.
// Non-user session kinds (global, unknown, cron, subagent) are excluded
// from default listings unless explicitly opted in.
func filterSessions(sessions []*session.Session, agentID, spawnedBy string, includeGlobal, includeUnknown *bool) []*session.Session {
	result := make([]*session.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Kind == session.KindGlobal && !isTrue(includeGlobal) {
			continue
		}
		if s.Kind == session.KindUnknown && !isTrue(includeUnknown) {
			continue
		}
		// Internal session kinds (cron, subagent) are excluded
		// from user-facing session listings by default.
		if s.Kind.IsInternal() {
			continue
		}
		if spawnedBy != "" && s.SpawnedBy != spawnedBy {
			continue
		}
		if agentID != "" && !matchesAgentID(s.Key, agentID) {
			continue
		}
		result = append(result, s)
	}
	return result
}

// matchesAgentID checks if a session key belongs to the given agent.
// Non-default agents use keys prefixed with "agent:<agentId>:".
func matchesAgentID(key, agentID string) bool {
	if agentID == "default" {
		return !strings.HasPrefix(key, "agent:")
	}
	return strings.HasPrefix(key, "agent:"+agentID+":")
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
