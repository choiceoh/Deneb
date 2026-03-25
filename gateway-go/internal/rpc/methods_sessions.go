package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SessionDeps extends Deps with a Forwarder for bridge-delegated session ops.
type SessionDeps struct {
	Deps
	Forwarder Forwarder
}

// RegisterSessionMethods registers Phase 3 session RPC methods.
func RegisterSessionMethods(d *Dispatcher, deps SessionDeps) {
	d.Register("sessions.patch", sessionsPatch(deps))
	d.Register("sessions.reset", sessionsReset(deps))
	d.Register("sessions.preview", sessionsPreview(deps))
	d.Register("sessions.resolve", sessionsResolve(deps))
	d.Register("sessions.compact", sessionsCompact(deps))
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

func sessionsPatch(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
			session.PatchFields
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		key := requireKey(p.Key)
		if key == "" {
			return errMissingKey(req.ID)
		}

		updated := deps.Sessions.Patch(key, p.PatchFields)

		// Forward to bridge for persistent store update; bridge response
		// includes resolved model info the Go layer doesn't have.
		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			emitSessionLifecycle(deps.Deps, key, "patch")
			return resp
		}

		emitSessionLifecycle(deps.Deps, key, "patch")
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

func sessionsReset(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key    string `json:"key"`
			Reason string `json:"reason"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		key := requireKey(p.Key)
		if key == "" {
			return errMissingKey(req.ID)
		}
		reason := "reset"
		if p.Reason == "new" {
			reason = "new"
		}

		// Forward to bridge for transcript archival + persistent store reset.
		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			deps.Sessions.ResetSession(key)
			emitSessionLifecycle(deps.Deps, key, reason)
			return resp
		}

		// Fallback: reset in-memory state only.
		s := deps.Sessions.ResetSession(key)
		if s == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "session not found: "+truncateForError(key)))
		}

		emitSessionLifecycle(deps.Deps, key, reason)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":    true,
			"key":   key,
			"entry": s,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// sessions.preview — forwarded to bridge (file I/O heavy)
// ---------------------------------------------------------------------------

func sessionsPreview(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Keys []string `json:"keys"`
		}
		if len(req.Params) > 0 {
			_ = unmarshalParams(req.Params, &p)
		}

		ts := time.Now().UnixMilli()
		keys := normalizeKeys(p.Keys, 64)

		if len(keys) == 0 {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ts":       ts,
				"previews": []any{},
			})
			return resp
		}

		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			return resp
		}

		// Fallback: return empty previews with "missing" status.
		previews := make([]map[string]any, len(keys))
		for i, k := range keys {
			previews[i] = map[string]any{
				"key":    k,
				"status": "missing",
				"items":  []any{},
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

func sessionsResolve(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key            string `json:"key"`
			SessionID      string `json:"sessionId"`
			Label          string `json:"label"`
			AgentID        string `json:"agentId"`
			SpawnedBy      string `json:"spawnedBy"`
			IncludeGlobal  *bool  `json:"includeGlobal"`
			IncludeUnknown *bool  `json:"includeUnknown"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}

		idCount := boolCount(p.Key != "", p.SessionID != "", p.Label != "")
		if idCount == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "one of key, sessionId, or label is required"))
		}
		if idCount > 1 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "provide exactly one of key, sessionId, or label"))
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
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrConflict, "ambiguous label: multiple sessions match"))
			}
		}

		if found != nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ok":  true,
				"key": found.Key,
			})
			return resp
		}

		// Fall back to bridge for persistent store lookup.
		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			return resp
		}

		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrNotFound, "session not found"))
	}
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

// ---------------------------------------------------------------------------
// sessions.compact — forwarded to bridge (file I/O heavy)
// ---------------------------------------------------------------------------

func sessionsCompact(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		key := requireKey(p.Key)
		if key == "" {
			return errMissingKey(req.ID)
		}

		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			// If compaction succeeded, clear in-memory token fields.
			var result struct {
				Compacted bool `json:"compacted"`
			}
			if resp.Payload != nil {
				_ = json.Unmarshal(resp.Payload, &result)
			}
			if result.Compacted {
				deps.Sessions.ClearTokens(key)
				emitSessionLifecycle(deps.Deps, key, "compact")
			}
			return resp
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":        true,
			"key":       key,
			"compacted": false,
			"reason":    "no bridge available",
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireKey trims whitespace from a session key; returns "" if empty.
func requireKey(key string) string {
	return strings.TrimSpace(key)
}

// errMissingKey returns a standard MISSING_PARAM error for the key field.
func errMissingKey(reqID string) *protocol.ResponseFrame {
	return protocol.NewResponseError(reqID, protocol.NewError(
		protocol.ErrMissingParam, "key is required"))
}

// forwardToBridge forwards an RPC request to the Node.js bridge.
// Returns the bridge response on success, or nil if the bridge is
// unavailable or returns an error (caller should fall through to local logic).
func forwardToBridge(ctx context.Context, fwd Forwarder, req *protocol.RequestFrame) *protocol.ResponseFrame {
	if fwd == nil {
		return nil
	}
	resp, err := fwd.Forward(ctx, req)
	if err != nil || resp == nil || resp.Error != nil {
		return nil
	}
	return resp
}

// emitSessionLifecycle emits a lifecycle change event if GatewaySubs is available.
func emitSessionLifecycle(deps Deps, sessionKey, reason string) {
	if deps.GatewaySubs != nil {
		deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
			SessionKey: sessionKey,
			Reason:     reason,
		})
	}
}

// normalizeKeys trims whitespace from keys, drops empty entries, and caps at max.
func normalizeKeys(raw []string, max int) []string {
	keys := make([]string, 0, len(raw))
	for _, k := range raw {
		if trimmed := strings.TrimSpace(k); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) > max {
		keys = keys[:max]
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
