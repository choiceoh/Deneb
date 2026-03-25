package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SessionExecDeps holds the dependencies for native session execution
// and agent RPC methods (sessions.send/steer/abort, agent, agent.identity.get, agent.wait).
type SessionExecDeps struct {
	Chat       *chat.Handler
	Agents     *agent.Store
	JobTracker *agent.JobTracker
}

// RegisterSessionExecMethods registers native Go handlers for session execution
// and agent methods that were previously bridge-forwarded to Node.js.
func RegisterSessionExecMethods(d *Dispatcher, deps SessionExecDeps) {
	if deps.Chat == nil {
		return
	}

	// Session messaging methods — native agent execution.
	d.Register("sessions.send", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.SessionsSend(ctx, req)
	})
	d.Register("sessions.steer", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.SessionsSteer(ctx, req)
	})
	d.Register("sessions.abort", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.SessionsAbort(ctx, req)
	})

	// Agent run method — delegates to sessions.send with agent context.
	d.Register("agent", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.SessionsSend(ctx, req)
	})

	// Agent identity — returns agent metadata from the store.
	d.Register("agent.identity.get", agentIdentityGet(deps))

	// Agent wait — waits for a running agent job to complete.
	d.Register("agent.wait", agentWait(deps))
}

func agentIdentityGet(deps SessionExecDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.AgentID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId is required"))
		}

		if deps.Agents == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "agent store not available"))
		}

		ag := deps.Agents.Get(p.AgentID)
		if ag == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "agent not found: "+p.AgentID))
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

func agentWait(deps SessionExecDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RunID        string `json:"runId"`
			TimeoutMs    int64  `json:"timeoutMs,omitempty"`
			IgnoreCached bool   `json:"ignoreCached,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RunID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "runId is required"))
		}

		if deps.JobTracker == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "job tracker not available"))
		}

		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 60_000 // Default 60s timeout.
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
