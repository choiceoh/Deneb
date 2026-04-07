// Package handlertask provides RPC handlers for the unified background task
// control plane. Methods cover task CRUD, flow management, audit, and status.
package handlertask

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for task RPC methods.
type Deps struct {
	Registry *tasks.Registry
}

// Methods returns all task-related RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Registry == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		// Task queries.
		"task.status": taskStatus(deps),
		"task.list":   taskList(deps),
		"task.get":    taskGet(deps),
		"task.events": taskEvents(deps),
		"task.cancel": taskCancel(deps),
		"task.audit":  taskAudit(deps),

		// Flow management.
		"flow.list":   flowList(deps),
		"flow.show":   flowShow(deps),
		"flow.cancel": flowCancel(deps),
	}
}

// --- task.status ---

func taskStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		sum := deps.Registry.Summary()
		return rpcutil.RespondOK(req.ID, sum)
	}
}

// --- task.list ---

type taskListParams struct {
	Runtime string `json:"runtime,omitempty"`
	Status  string `json:"status,omitempty"`
	Owner   string `json:"owner,omitempty"`
	FlowID  string `json:"flowId,omitempty"`
	Active  bool   `json:"active,omitempty"`
}

func taskList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p taskListParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}

		var list []*tasks.TaskRecord

		switch {
		case p.Active:
			list = deps.Registry.ListActive()
		case p.Runtime != "":
			list = deps.Registry.ListByRuntime(tasks.TaskRuntime(p.Runtime))
		case p.Owner != "":
			list = deps.Registry.ListByOwner(p.Owner)
		case p.FlowID != "":
			list = deps.Registry.ListByFlowID(p.FlowID)
		default:
			list = deps.Registry.ListAll()
		}

		// Filter by status if specified.
		if p.Status != "" {
			st := tasks.TaskStatus(p.Status)
			filtered := list[:0]
			for _, t := range list {
				if t.Status == st {
					filtered = append(filtered, t)
				}
			}
			list = filtered
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"tasks": list,
			"count": len(list),
		})
	}
}

// --- task.get ---

type taskGetParams struct {
	TaskID string `json:"taskId"`
	RunID  string `json:"runId"`
}

func taskGet(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p taskGetParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}

		var t *tasks.TaskRecord
		switch {
		case p.TaskID != "":
			t = deps.Registry.Get(p.TaskID)
		case p.RunID != "":
			t = deps.Registry.GetByRunID(p.RunID)
		default:
			return rpcerr.New(protocol.ErrMissingParam, "taskId or runId required").Response(req.ID)
		}

		if t == nil {
			return rpcerr.NotFound("task").Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, t)
	}
}

// --- task.events ---

type taskEventsParams struct {
	TaskID string `json:"taskId"`
}

func taskEvents(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p taskEventsParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.TaskID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "taskId required").Response(req.ID)
		}

		events, err := deps.Registry.ListEvents(p.TaskID)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrUnavailable, err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"taskId": p.TaskID,
			"events": events,
		})
	}
}

// --- task.cancel ---

type taskCancelParams struct {
	TaskID string `json:"taskId"`
}

func taskCancel(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p taskCancelParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.TaskID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "taskId required").Response(req.ID)
		}

		if err := tasks.CancelTask(deps.Registry, p.TaskID); err != nil {
			return rpcerr.Wrap(protocol.ErrUnavailable, err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"cancelled": true,
			"taskId":    p.TaskID,
		})
	}
}

// --- task.audit ---

func taskAudit(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		summary := tasks.RunAudit(deps.Registry, tasks.AuditOptions{})
		return rpcutil.RespondOK(req.ID, summary)
	}
}

// --- flow.list ---

type flowListParams struct {
	Active bool `json:"active,omitempty"`
}

func flowList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p flowListParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}

		var list []*tasks.FlowRecord
		if p.Active {
			list = deps.Registry.ListActiveFlows()
		} else {
			list = deps.Registry.ListFlows()
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"flows": list,
			"count": len(list),
		})
	}
}

// --- flow.show ---

type flowShowParams struct {
	FlowID string `json:"flowId"`
}

func flowShow(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p flowShowParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.FlowID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "flowId required").Response(req.ID)
		}

		flow := deps.Registry.GetFlow(p.FlowID)
		if flow == nil {
			return rpcerr.NotFound("flow").Response(req.ID)
		}

		flowTasks := deps.Registry.ListByFlowID(p.FlowID)

		return rpcutil.RespondOK(req.ID, map[string]any{
			"flow":  flow,
			"tasks": flowTasks,
		})
	}
}

// --- flow.cancel ---

type flowCancelParams struct {
	FlowID string `json:"flowId"`
}

func flowCancel(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p flowCancelParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.FlowID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "flowId required").Response(req.ID)
		}

		cancelled, err := tasks.CancelFlow(deps.Registry, p.FlowID)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrNotFound, err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"cancelled":      true,
			"flowId":         p.FlowID,
			"tasksCancelled": cancelled,
		})
	}
}
