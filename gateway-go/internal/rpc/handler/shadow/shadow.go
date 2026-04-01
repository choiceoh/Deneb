// Package shadow provides RPC handlers for the shadow monitoring service.
package shadow

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	shadowsvc "github.com/choiceoh/deneb/gateway-go/internal/shadow"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for shadow RPC methods.
type Deps struct {
	Shadow *shadowsvc.Service
}

// Methods returns all shadow-related RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Shadow == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"shadow.status":       handleStatus(deps),
		"shadow.tasks":        handleTasks(deps),
		"shadow.task.dismiss": handleDismiss(deps),
	}
}

// --- shadow.status ---

func handleStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, deps.Shadow.Status())
	}
}

// --- shadow.tasks ---

func handleTasks(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		tasks := deps.Shadow.PendingTasks()
		if tasks == nil {
			tasks = []shadowsvc.TrackedTask{}
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"tasks": tasks,
			"count": len(tasks),
		})
	}
}

// --- shadow.task.dismiss ---

type dismissParams struct {
	ID string `json:"id"`
}

func handleDismiss(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p dismissParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.ID == "" {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "id required"))
		}

		ok := deps.Shadow.DismissTask(p.ID)
		if !ok {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrNotFound, "task not found or already dismissed"))
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"dismissed": true,
			"id":        p.ID,
		})
	}
}
