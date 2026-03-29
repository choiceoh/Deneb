// Package session provides RPC handlers for session management methods.
//
// Methods: sessions.patch, sessions.reset, sessions.preview, sessions.resolve,
// sessions.compact, sessions.repair, sessions.overflow_check, sessions.send,
// sessions.steer, sessions.abort, agent, agent.identity.get, agent.wait.
package session

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for session RPC methods.
type Deps struct {
	Sessions         *session.Manager
	Channels         interface{} // *channel.Registry — unused by session handlers but kept for parity.
	ChannelLifecycle interface{} // *channel.LifecycleManager — unused by session handlers.
	GatewaySubs      *events.GatewayEventSubscriptions
	Version          string
	Transcripts      *transcript.Writer
	Compressor       *transcript.Compressor
}

// ExecDeps holds dependencies for native session execution and agent RPC methods.
type ExecDeps struct {
	Chat       *chat.Handler
	Agents     *agent.Store
	JobTracker *agent.JobTracker
}

// Methods returns all session management RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"sessions.patch":          sessionsPatch(deps),
		"sessions.reset":         sessionsReset(deps),
		"sessions.preview":       sessionsPreview(deps),
		"sessions.resolve":       sessionsResolve(deps),
		"sessions.compact":       sessionsCompact(deps),
		"sessions.repair":        sessionsRepair(deps),
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
		"agent.identity.get": agentIdentityGet(deps),
		"agent.wait":         agentWait(deps),
	}
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

func sessionsPatch(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
			session.PatchFields
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}

		updated := deps.Sessions.Patch(key, p.PatchFields)
		emitSessionLifecycle(deps.GatewaySubs, key, "patch")
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":    true,
			"key":   key,
			"entry": updated,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// sessions.reset
// ---------------------------------------------------------------------------

func sessionsReset(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key    string `json:"key"`
			Reason string `json:"reason"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}
		reason := "reset"
		if p.Reason == "new" {
			reason = "new"
		}

		s := deps.Sessions.ResetSession(key)
		if s == nil {
			return rpcerr.NotFound("session").
				WithSession(rpcutil.TruncateForError(key)).
				Response(req.ID)
		}

		emitSessionLifecycle(deps.GatewaySubs, key, reason)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":    true,
			"key":   key,
			"entry": s,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// sessions.preview — loads transcript preview from JSONL files
// ---------------------------------------------------------------------------

func sessionsPreview(deps Deps) rpcutil.HandlerFunc {
	const defaultMaxItems = 10

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Keys     []string `json:"keys"`
			MaxItems int      `json:"maxItems,omitempty"`
		}
		if len(req.Params) > 0 {
			_ = rpcutil.UnmarshalParams(req.Params, &p)
		}

		ts := time.Now().UnixMilli()
		keys := normalizeKeys(p.Keys, 64)
		maxItems := p.MaxItems
		if maxItems <= 0 || maxItems > 50 {
			maxItems = defaultMaxItems
		}

		if len(keys) == 0 {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ts":       ts,
				"previews": []any{},
			})
			return resp
		}

		previews := make([]map[string]any, len(keys))
		for i, k := range keys {
			status := "missing"
			var items any = []any{}

			if deps.Transcripts != nil {
				preview, err := deps.Transcripts.ReadPreview(k, maxItems)
				if err == nil && preview != nil {
					status = "ok"
					items = preview
				}
			}

			previews[i] = map[string]any{
				"key":    k,
				"status": status,
				"items":  items,
			}
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ts":       ts,
			"previews": previews,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// sessions.resolve
// ---------------------------------------------------------------------------

func sessionsResolve(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key            string `json:"key"`
			SessionID      string `json:"sessionId"`
			Label          string `json:"label"`
			AgentID        string `json:"agentId"`
			SpawnedBy      string `json:"spawnedBy"`
			IncludeGlobal  *bool  `json:"includeGlobal"`
			IncludeUnknown *bool  `json:"includeUnknown"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}

		idCount := boolCount(p.Key != "", p.SessionID != "", p.Label != "")
		if idCount == 0 {
			return rpcerr.MissingParam("key, sessionId, or label").Response(req.ID)
		}
		if idCount > 1 {
			return rpcerr.New(protocol.ErrInvalidRequest, "provide exactly one of key, sessionId, or label").
				Response(req.ID)
		}

		// TS behavior: key lookup is direct (no kind filter); sessionId and
		// label lookups apply includeGlobal/includeUnknown filters (default false).
		var found *session.Session
		switch {
		case p.Key != "":
			found = deps.Sessions.Get(strings.TrimSpace(p.Key))
		case p.SessionID != "":
			if s := deps.Sessions.FindBySessionID(p.SessionID); s != nil {
				if filtered := filterSessions([]*session.Session{s}, p.AgentID, p.SpawnedBy, p.IncludeGlobal, p.IncludeUnknown); len(filtered) == 1 {
					found = filtered[0]
				}
			}
		case p.Label != "":
			matches := filterSessions(
				deps.Sessions.FindByLabel(p.Label),
				p.AgentID, p.SpawnedBy, p.IncludeGlobal, p.IncludeUnknown,
			)
			if len(matches) == 1 {
				found = matches[0]
			} else if len(matches) > 1 {
				return rpcerr.Conflict("ambiguous label: multiple sessions match").
					Response(req.ID)
			}
		}

		if found != nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ok":  true,
				"key": found.Key,
			})
			return resp
		}

		return rpcerr.NotFound("session").Response(req.ID)
	}
}

// ---------------------------------------------------------------------------
// sessions.compact — compresses session transcript via Compressor
// ---------------------------------------------------------------------------

func sessionsCompact(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}

		if deps.Compressor == nil || deps.Transcripts == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ok":        true,
				"key":       key,
				"compacted": false,
				"reason":    "transcript compressor not available",
			})
			return resp
		}

		result, err := deps.Compressor.Compact(key, deps.Transcripts)
		if err != nil {
			return rpcerr.Unavailable("compaction failed: " + err.Error()).
				WithSession(key).
				Response(req.ID)
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":        result.OK,
			"key":       key,
			"compacted": result.Compacted,
			"reason":    result.Reason,
			"stats": map[string]any{
				"originalMessages": result.OriginalMessages,
				"retainedMessages": result.RetainedMessages,
				"summaryCount":     result.SummaryCount,
			},
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// sessions.repair — triggers post-compaction transcript repair
// ---------------------------------------------------------------------------

func sessionsRepair(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionKey string `json:"sessionKey"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.MissingParam("sessionKey").
				WithMethod("sessions.repair").
				Response(req.ID)
		}

		if p.SessionKey == "" {
			return rpcerr.MissingParam("sessionKey").
				WithMethod("sessions.repair").
				Response(req.ID)
		}

		// Verify session exists in the session manager.
		s := deps.Sessions.Get(p.SessionKey)
		if s == nil {
			return rpcerr.NotFound("session").
				WithSession(p.SessionKey).
				Response(req.ID)
		}

		// In native Go mode, transcript repair is handled by the session
		// manager's compaction pipeline. Return success to indicate the
		// session is valid and repair can proceed.
		return protocol.MustResponseOK(req.ID, map[string]any{
			"sessionKey": p.SessionKey,
			"status":     "repair_queued",
		})
	}
}

// ---------------------------------------------------------------------------
// sessions.overflow_check — checks context overflow state
// ---------------------------------------------------------------------------

func sessionsOverflowCheck(_ Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionKey    string `json:"sessionKey"`
			CurrentTokens int64  `json:"currentTokens"`
			MaxTokens     int64  `json:"maxTokens"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.MissingParam("params").
				WithMethod("sessions.overflow_check").
				Response(req.ID)
		}

		if p.MaxTokens <= 0 {
			return protocol.MustResponseOK(req.ID, map[string]any{
				"isOverflow": false,
				"usage":      0.0,
			})
		}

		usage := float64(p.CurrentTokens) / float64(p.MaxTokens)
		isOverflow := usage > 0.9 // 90% threshold

		return protocol.MustResponseOK(req.ID, map[string]any{
			"isOverflow":          isOverflow,
			"usage":               usage,
			"emergencyPruneRatio": minf(maxf((usage-0.7)/usage, 0), 0.5),
		})
	}
}

// ---------------------------------------------------------------------------
// agent.identity.get
// ---------------------------------------------------------------------------

func agentIdentityGet(deps ExecDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			AgentID string `json:"agentId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.AgentID == "" {
			return rpcerr.MissingParam("agentId").
				WithMethod("agent.identity.get").
				Response(req.ID)
		}
		if deps.Agents == nil {
			return rpcerr.NotFound("agent store").
				WithMethod("agent.identity.get").
				Response(req.ID)
		}
		ag := deps.Agents.Get(p.AgentID)
		if ag == nil {
			return rpcerr.NotFound("agent").
				WithAgent(p.AgentID).
				Response(req.ID)
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"agentId":      ag.AgentID,
			"name":         ag.Name,
			"description":  ag.Description,
			"model":        ag.Model,
			"systemPrompt": ag.SystemPrompt,
			"tools":        ag.Tools,
			"metadata":     ag.Metadata,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// agent.wait
// ---------------------------------------------------------------------------

func agentWait(deps ExecDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			RunID        string `json:"runId"`
			TimeoutMs    int64  `json:"timeoutMs,omitempty"`
			IgnoreCached bool   `json:"ignoreCached,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.RunID == "" {
			return rpcerr.MissingParam("runId").
				WithMethod("agent.wait").
				Response(req.ID)
		}
		if deps.JobTracker == nil {
			return rpcerr.Unavailable("job tracker not available").
				WithMethod("agent.wait").
				Response(req.ID)
		}
		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 60_000
		}
		snapshot := deps.JobTracker.WaitForJob(ctx, p.RunID, p.TimeoutMs, p.IgnoreCached)
		if snapshot == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"status":  "timeout",
				"message": "job did not complete within timeout",
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, snapshot)
		return resp
	}
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
// Globals and unknowns are excluded unless explicitly opted in (matching TS
// behavior where includeGlobal/includeUnknown default to false).
func filterSessions(sessions []*session.Session, agentID, spawnedBy string, includeGlobal, includeUnknown *bool) []*session.Session {
	result := make([]*session.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Kind == session.KindGlobal && !isTrue(includeGlobal) {
			continue
		}
		if s.Kind == session.KindUnknown && !isTrue(includeUnknown) {
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
