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
		key := normalizeKey(p.Key)
		if key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}

		// Apply patch to in-memory session.
		updated := deps.Sessions.Patch(key, p.PatchFields)

		// Forward to bridge for persistent store update.
		if deps.Forwarder != nil {
			forwardReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID,
				Method: "sessions.patch",
				Params: req.Params,
			}
			bridgeResp, err := deps.Forwarder.Forward(ctx, forwardReq)
			if err == nil && bridgeResp != nil && bridgeResp.Error == nil {
				// Bridge succeeded — emit lifecycle and return bridge response
				// (which includes resolved model info).
				emitSessionLifecycle(deps.Deps, key, "patch")
				return bridgeResp
			}
			// On bridge failure, fall through to return in-memory result.
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
		key := normalizeKey(p.Key)
		if key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		reason := "reset"
		if p.Reason == "new" {
			reason = "new"
		}

		// Forward to bridge for transcript archival + persistent store reset.
		if deps.Forwarder != nil {
			forwardReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID,
				Method: "sessions.reset",
				Params: req.Params,
			}
			bridgeResp, err := deps.Forwarder.Forward(ctx, forwardReq)
			if err == nil && bridgeResp != nil && bridgeResp.Error == nil {
				// Also reset in-memory state.
				deps.Sessions.ResetSession(key)
				emitSessionLifecycle(deps.Deps, key, reason)
				return bridgeResp
			}
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
			Keys     []string `json:"keys"`
			Limit    *int     `json:"limit"`
			MaxChars *int     `json:"maxChars"`
		}
		if len(req.Params) > 0 {
			_ = unmarshalParams(req.Params, &p)
		}

		// Normalize keys.
		keys := make([]string, 0, len(p.Keys))
		for _, k := range p.Keys {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				keys = append(keys, trimmed)
			}
		}
		if len(keys) > 64 {
			keys = keys[:64]
		}
		if len(keys) == 0 {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"ts":       time.Now().UnixMilli(),
				"previews": []any{},
			})
			return resp
		}

		// Forward to bridge — transcript file I/O is Node.js managed.
		if deps.Forwarder != nil {
			forwardReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID,
				Method: "sessions.preview",
				Params: req.Params,
			}
			bridgeResp, err := deps.Forwarder.Forward(ctx, forwardReq)
			if err == nil && bridgeResp != nil {
				return bridgeResp
			}
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
			"ts":       time.Now().UnixMilli(),
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

		// Count how many identifiers were provided.
		idCount := 0
		if p.Key != "" {
			idCount++
		}
		if p.SessionID != "" {
			idCount++
		}
		if p.Label != "" {
			idCount++
		}
		if idCount == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "one of key, sessionId, or label is required"))
		}
		if idCount > 1 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "provide exactly one of key, sessionId, or label"))
		}

		// Try in-memory resolution first.
		// TS behavior: key lookup is direct (no kind filter); sessionId and
		// label lookups apply includeGlobal/includeUnknown filters (default false).
		var found *session.Session
		switch {
		case p.Key != "":
			// Direct key lookup — no kind filtering (matches TS).
			found = deps.Sessions.Get(strings.TrimSpace(p.Key))
		case p.SessionID != "":
			s := deps.Sessions.FindBySessionID(p.SessionID)
			if s != nil {
				filtered := filterSessions([]*session.Session{s}, p.AgentID, p.SpawnedBy, p.IncludeGlobal, p.IncludeUnknown)
				if len(filtered) == 1 {
					found = filtered[0]
				}
			}
		case p.Label != "":
			matches := deps.Sessions.FindByLabel(p.Label)
			matches = filterSessions(matches, p.AgentID, p.SpawnedBy, p.IncludeGlobal, p.IncludeUnknown)
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
		if deps.Forwarder != nil {
			forwardReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID,
				Method: "sessions.resolve",
				Params: req.Params,
			}
			bridgeResp, err := deps.Forwarder.Forward(ctx, forwardReq)
			if err == nil && bridgeResp != nil {
				return bridgeResp
			}
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
		// Exclude globals unless explicitly includeGlobal=true.
		if s.Kind == session.KindGlobal && (includeGlobal == nil || !*includeGlobal) {
			continue
		}
		// Exclude unknowns unless explicitly includeUnknown=true.
		if s.Kind == session.KindUnknown && (includeUnknown == nil || !*includeUnknown) {
			continue
		}
		if spawnedBy != "" && s.SpawnedBy != spawnedBy {
			continue
		}
		// agentID filter: match against the session key prefix convention.
		// Session keys for non-default agents are prefixed with "agent:<agentId>:".
		if agentID != "" {
			prefix := "agent:" + agentID + ":"
			keyMatchesAgent := strings.HasPrefix(s.Key, prefix)
			if agentID == "default" {
				keyMatchesAgent = !strings.HasPrefix(s.Key, "agent:")
			}
			if !keyMatchesAgent {
				continue
			}
		}
		result = append(result, s)
	}
	return result
}

// ---------------------------------------------------------------------------
// sessions.compact — forwarded to bridge (file I/O heavy)
// ---------------------------------------------------------------------------

func sessionsCompact(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key      string `json:"key"`
			MaxLines *int   `json:"maxLines"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		key := normalizeKey(p.Key)
		if key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}

		// Forward to bridge for transcript file compaction.
		if deps.Forwarder != nil {
			forwardReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID,
				Method: "sessions.compact",
				Params: req.Params,
			}
			bridgeResp, err := deps.Forwarder.Forward(ctx, forwardReq)
			if err == nil && bridgeResp != nil && bridgeResp.Error == nil {
				// If compaction succeeded, clear in-memory token fields.
				var result struct {
					Compacted bool `json:"compacted"`
				}
				if bridgeResp.Payload != nil {
					_ = json.Unmarshal(bridgeResp.Payload, &result)
				}
				if result.Compacted {
					deps.Sessions.ClearTokens(key)
					emitSessionLifecycle(deps.Deps, key, "compact")
				}
				return bridgeResp
			}
		}

		// Without bridge, return not-compacted.
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

// normalizeKey trims whitespace from a session key.
func normalizeKey(key string) string {
	return strings.TrimSpace(key)
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
