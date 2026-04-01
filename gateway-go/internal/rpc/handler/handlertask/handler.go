// Package handlertask provides RPC handlers for the unified background task
// control plane. Methods cover task CRUD, flow management, audit, and status.
package handlertask

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/tasks"
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
		"task.status":  taskStatus(deps),
		"task.list":    taskList(deps),
		"task.get":     taskGet(deps),
		"task.events":  taskEvents(deps),
		"task.cancel":  taskCancel(deps),
		"task.audit":   taskAudit(deps),

		// Flow management.
		"flow.list":    flowList(deps),
		"flow.show":    flowShow(deps),
		"flow.cancel":  flowCancel(deps),
	}
}

// --- task.status ---

func taskStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		sum := deps.Registry.Summary()
		return protocol.NewResponseResult(req.ID, sum)
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

		return protocol.NewResponseResult(req.ID, map[string]any{
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
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "taskId or runId required"))
		}

		if t == nil {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrNotFound, "task not found"))
		}

		return protocol.NewResponseResult(req.ID, t)
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
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "taskId required"))
		}

		events, err := deps.Registry.ListEvents(p.TaskID)
		if err != nil {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrUnavailable, err.Error()))
		}

		return protocol.NewResponseResult(req.ID, map[string]any{
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
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "taskId required"))
		}

		if err := tasks.CancelTask(deps.Registry, p.TaskID); err != nil {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrUnavailable, err.Error()))
		}

		return protocol.NewResponseResult(req.ID, map[string]any{
			"cancelled": true,
			"taskId":    p.TaskID,
		})
	}
}

// --- task.audit ---

func taskAudit(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		summary := tasks.RunAudit(deps.Registry, tasks.AuditOptions{})
		return protocol.NewResponseResult(req.ID, summary)
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

		return protocol.NewResponseResult(req.ID, map[string]any{
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
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "flowId required"))
		}

		flow := deps.Registry.GetFlow(p.FlowID)
		if flow == nil {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrNotFound, "flow not found"))
		}

		flowTasks := deps.Registry.ListByFlowID(p.FlowID)

		return protocol.NewResponseResult(req.ID, map[string]any{
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
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrMissingParam, "flowId required"))
		}

		flow := deps.Registry.GetFlow(p.FlowID)
		if flow == nil {
			return protocol.NewResponseError(req.ID,
				protocol.NewError(protocol.ErrNotFound, "flow not found"))
		}

		// Cancel all active tasks in this flow.
		flowTasks := deps.Registry.ListByFlowID(p.FlowID)
		cancelled := 0
		for _, t := range flowTasks {
			if t.Status.IsActive() {
				if err := tasks.CancelTask(deps.Registry, t.TaskID); err == nil {
					cancelled++
				}
			}
		}

		// Mark the flow as cancelled.
		flow.Status = tasks.FlowCancelled
		flow.UpdatedAt = tasks.NowMs()
		flow.CompletedAt = tasks.NowMs()
		_ = deps.Registry.PutFlow(flow)

		return protocol.NewResponseResult(req.ID, map[string]any{
			"cancelled":      true,
			"flowId":         p.FlowID,
			"tasksCancelled": cancelled,
		})
	}
}
