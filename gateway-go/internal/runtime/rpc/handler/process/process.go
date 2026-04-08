// Package process contains RPC handlers for exec approval, ACP (Agent
// Communication Protocol), and advanced cron operations. These were migrated
// from the flat rpc package into a domain-based subpackage.
package process

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

// ApprovalDeps holds the dependencies for exec approval RPC methods.
type ApprovalDeps struct {
	Store       *approval.Store
	Broadcaster BroadcastFunc
}

// ApprovalMethods returns exec.approval.* and exec.approvals.* RPC handlers.
func ApprovalMethods(deps ApprovalDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"exec.approval.request":      execApprovalRequest(deps),
		"exec.approval.waitDecision": execApprovalWaitDecision(deps),
		"exec.approval.resolve":      execApprovalResolve(deps),
		"exec.approvals.get":         execApprovalsGet(deps),
		"exec.approvals.set":         execApprovalsSet(deps),
	}
}

func execApprovalRequest(deps ApprovalDeps) rpcutil.HandlerFunc {
	type params struct {
		ID                  string            `json:"id,omitempty"`
		Command             string            `json:"command"`
		CommandArgv         []string          `json:"commandArgv,omitempty"`
		Env                 map[string]string `json:"env,omitempty"`
		Cwd                 string            `json:"cwd,omitempty"`
		SystemRunPlan       any               `json:"systemRunPlan,omitempty"`
		Host                string            `json:"host,omitempty"`
		Security            string            `json:"security,omitempty"`
		Ask                 string            `json:"ask,omitempty"`
		AgentID             string            `json:"agentId,omitempty"`
		ResolvedPath        string            `json:"resolvedPath,omitempty"`
		SessionKey          string            `json:"sessionKey,omitempty"`
		TimeoutMs           int64             `json:"timeoutMs,omitempty"`
		TwoPhase            bool              `json:"twoPhase,omitempty"`
		TurnSourceChannel   string            `json:"turnSourceChannel,omitempty"`
		TurnSourceTo        string            `json:"turnSourceTo,omitempty"`
		TurnSourceAccountID string            `json:"turnSourceAccountId,omitempty"`
		TurnSourceThreadID  string            `json:"turnSourceThreadId,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Command == "" {
			return nil, rpcerr.MissingParam("command")
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
			})
		}

		if p.TwoPhase {
			return map[string]any{
				"status":      "accepted",
				"id":          created.ID,
				"createdAtMs": created.CreatedAtMs,
				"expiresAtMs": created.ExpiresAtMs,
			}, nil
		}

		return created, nil
	})
}

func execApprovalWaitDecision(deps ApprovalDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		existing := deps.Store.Get(p.ID)
		if existing == nil {
			return rpcerr.NotFound("approval request").Response(req.ID)
		}
		if existing.Decision != nil {
			return rpcutil.RespondOK(req.ID, existing)
		}

		// Wait for decision or context cancellation.
		ch := deps.Store.WaitForDecision(p.ID)
		select {
		case <-ch:
			result := deps.Store.Get(p.ID)
			if result == nil {
				return rpcerr.New(protocol.ErrNotFound, "approval request expired").Response(req.ID)
			}
			return rpcutil.RespondOK(req.ID, result)
		case <-ctx.Done():
			return rpcerr.New(protocol.ErrAgentTimeout, "wait for decision timed out").Response(req.ID)
		}
	}
}

func execApprovalResolve(deps ApprovalDeps) rpcutil.HandlerFunc {
	type params struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" || p.Decision == "" {
			return nil, rpcerr.New(protocol.ErrMissingParam, "id and decision are required")
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
			return nil, rpcerr.New(protocol.ErrValidationFailed, "invalid decision: must be allow-once, allow-always, or deny")
		}

		if err := deps.Store.Resolve(p.ID, decision); err != nil {
			return nil, rpcerr.Wrap(protocol.ErrNotFound, err)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("exec.approval.resolved", map[string]any{
				"id":       p.ID,
				"decision": p.Decision,
			})
		}

		return map[string]bool{"ok": true}, nil
	})
}

func execApprovalsGet(deps ApprovalDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot := deps.Store.GlobalSnapshot()
		if snapshot == nil {
			snapshot = &approval.Snapshot{
				File:     approval.ApprovalsFile{Version: 1},
				LoadedAt: 0,
			}
		}
		return rpcutil.RespondOK(req.ID, snapshot)
	}
}

func execApprovalsSet(deps ApprovalDeps) rpcutil.HandlerFunc {
	type params struct {
		File     approval.ApprovalsFile `json:"file"`
		BaseHash string                 `json:"baseHash,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		deps.Store.SetGlobalSnapshot(p.File, p.BaseHash)
		return map[string]bool{"ok": true}, nil
	})
}

// ---------------------------------------------------------------------------
// ACP (Agent Communication Protocol)
