package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ApprovalDeps holds the dependencies for exec approval RPC methods.
type ApprovalDeps struct {
	Store       *approval.Store
	Broadcaster BroadcastFunc
}

// BroadcastFunc broadcasts an event to subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// RegisterApprovalMethods registers exec.approval.* and exec.approvals.* RPC methods.
func RegisterApprovalMethods(d *Dispatcher, deps ApprovalDeps) {
	if deps.Store == nil {
		return
	}

	d.Register("exec.approval.request", execApprovalRequest(deps))
	d.Register("exec.approval.waitDecision", execApprovalWaitDecision(deps))
	d.Register("exec.approval.resolve", execApprovalResolve(deps))
	d.Register("exec.approvals.get", execApprovalsGet(deps))
	d.Register("exec.approvals.set", execApprovalsSet(deps))
	d.Register("exec.approvals.node.get", execApprovalsNodeGet(deps))
	d.Register("exec.approvals.node.set", execApprovalsNodeSet(deps))
}

func execApprovalRequest(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID           string            `json:"id,omitempty"`
			Command      string            `json:"command"`
			CommandArgv  []string          `json:"commandArgv,omitempty"`
			Env          map[string]string `json:"env,omitempty"`
			Cwd          string            `json:"cwd,omitempty"`
			SystemRunPlan any              `json:"systemRunPlan,omitempty"`
			NodeID       string            `json:"nodeId,omitempty"`
			Host         string            `json:"host,omitempty"`
			Security     string            `json:"security,omitempty"`
			Ask          string            `json:"ask,omitempty"`
			AgentID      string            `json:"agentId,omitempty"`
			ResolvedPath string            `json:"resolvedPath,omitempty"`
			SessionKey   string            `json:"sessionKey,omitempty"`
			TimeoutMs    int64             `json:"timeoutMs,omitempty"`
			TwoPhase     bool              `json:"twoPhase,omitempty"`
			TurnSourceChannel   string     `json:"turnSourceChannel,omitempty"`
			TurnSourceTo        string     `json:"turnSourceTo,omitempty"`
			TurnSourceAccountID string     `json:"turnSourceAccountId,omitempty"`
			TurnSourceThreadID  string     `json:"turnSourceThreadId,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Command == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "command is required"))
		}

		var turnSource *approval.TurnSourceInfo
		if p.TurnSourceChannel != "" {
			turnSource = &approval.TurnSourceInfo{
				Channel:   p.TurnSourceChannel,
				To:        p.TurnSourceTo,
				AccountID: p.TurnSourceAccountID,
				ThreadID:  p.TurnSourceThreadID,
			}
		}

		created := deps.Store.CreateRequest(approval.CreateRequestParams{
			ID:            p.ID,
			Command:       p.Command,
			CommandArgv:   p.CommandArgv,
			Env:           p.Env,
			Cwd:           p.Cwd,
			SystemRunPlan: p.SystemRunPlan,
			NodeID:        p.NodeID,
			Host:          p.Host,
			Security:      p.Security,
			Ask:           p.Ask,
			AgentID:       p.AgentID,
			ResolvedPath:  p.ResolvedPath,
			SessionKey:    p.SessionKey,
			TimeoutMs:     p.TimeoutMs,
			TwoPhase:      p.TwoPhase,
			TurnSource:    turnSource,
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("exec.approval.requested", map[string]any{
				"id":      created.ID,
				"command": created.Command,
				"nodeId":  created.NodeID,
			})
		}

		if p.TwoPhase {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"status":      "accepted",
				"id":          created.ID,
				"createdAtMs": created.CreatedAtMs,
				"expiresAtMs": created.ExpiresAtMs,
			})
			return resp
		}

		resp, _ := protocol.NewResponseOK(req.ID, created)
		return resp
	}
}

func execApprovalWaitDecision(deps ApprovalDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}

		existing := deps.Store.Get(p.ID)
		if existing == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "approval request not found"))
		}
		if existing.Decision != nil {
			resp, _ := protocol.NewResponseOK(req.ID, existing)
			return resp
		}

		// Wait for decision or context cancellation.
		ch := deps.Store.WaitForDecision(p.ID)
		select {
		case <-ch:
			result := deps.Store.Get(p.ID)
			if result == nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrNotFound, "approval request expired"))
			}
			resp, _ := protocol.NewResponseOK(req.ID, result)
			return resp
		case <-ctx.Done():
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrAgentTimeout, "wait for decision timed out"))
		}
	}
}

func execApprovalResolve(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID       string `json:"id"`
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.ID == "" || p.Decision == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id and decision are required"))
		}

		var decision approval.Decision
		switch p.Decision {
		case "allow-once":
			decision = approval.DecisionAllowOnce
		case "allow-always":
			decision = approval.DecisionAllowAlways
		case "deny":
			decision = approval.DecisionDeny
		default:
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid decision: must be allow-once, allow-always, or deny"))
		}

		if err := deps.Store.Resolve(p.ID, decision); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("exec.approval.resolved", map[string]any{
				"id":       p.ID,
				"decision": p.Decision,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func execApprovalsGet(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot := deps.Store.GetGlobalSnapshot()
		if snapshot == nil {
			snapshot = &approval.Snapshot{
				File:     approval.ApprovalsFile{Version: 1},
				LoadedAt: 0,
			}
		}
		resp, _ := protocol.NewResponseOK(req.ID, snapshot)
		return resp
	}
}

func execApprovalsSet(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			File     approval.ApprovalsFile `json:"file"`
			BaseHash string                 `json:"baseHash,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}

		deps.Store.SetGlobalSnapshot(p.File, p.BaseHash)

		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func execApprovalsNodeGet(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID string `json:"nodeId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.NodeID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId is required"))
		}

		snapshot := deps.Store.GetNodeSnapshot(p.NodeID)
		if snapshot == nil {
			snapshot = &approval.Snapshot{
				File:     approval.ApprovalsFile{Version: 1},
				LoadedAt: 0,
			}
		}
		resp, _ := protocol.NewResponseOK(req.ID, snapshot)
		return resp
	}
}

func execApprovalsNodeSet(deps ApprovalDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID   string                 `json:"nodeId"`
			File     approval.ApprovalsFile `json:"file"`
			BaseHash string                 `json:"baseHash,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.NodeID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId is required"))
		}

		deps.Store.SetNodeSnapshot(p.NodeID, p.File, p.BaseHash)

		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}
