// session_crud.go — sessions.list, sessions.get, sessions.delete RPC handlers.
package session

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CRUDMethods returns basic session CRUD handlers (list, get, delete).
// Uses the same Deps as session management methods.
func CRUDMethods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"sessions.list":   sessionsList(deps),
		"sessions.get":    sessionsGet(deps),
		"sessions.delete": sessionsDelete(deps),
	}
}

func sessionsList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Sessions.List())
	}
}

func sessionsGet(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Key string `json:"key"`
		}](req)
		if errResp != nil {
			return errResp
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}
		s := deps.Sessions.Get(key)
		if s == nil {
			return rpcerr.NotFound("session").
				WithSession(rpcutil.TruncateForError(key)).
				Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, s)
	}
}

func sessionsDelete(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Key   string `json:"key"`
			Force bool   `json:"force"`
		}](req)
		if errResp != nil {
			return errResp
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}
		s := deps.Sessions.Get(key)
		if s != nil && s.Status == session.StatusRunning && !p.Force {
			return rpcerr.Conflict("session is currently running; use force=true to delete").
				WithSession(key).
				Response(req.ID)
		}
		found := deps.Sessions.Delete(key)
		// Remove the persisted transcript too — a surviving .jsonl resurrects
		// the session at the next startup restore (mirrors
		// miniapp.sessions.delete). Best-effort, but never silent.
		if deps.Transcripts != nil {
			if store, err := deps.Transcripts(); err == nil && store != nil {
				if delErr := store.Delete(key); delErr != nil {
					slog.Warn("sessions.delete: transcript removal failed; session may resurrect on restart",
						"sessionKey", key, "error", delErr)
				}
			}
		}
		if found && deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: key,
				Reason:     "deleted",
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]bool{"deleted": found})
	}
}
