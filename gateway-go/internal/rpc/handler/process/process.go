// Package process contains RPC handlers for exec approval, ACP (Agent
// Communication Protocol), and advanced cron operations. These were migrated
// from the flat rpc package into a domain-based subpackage.
package process

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc broadcasts an event to subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

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
		"exec.approvals.node.get":    execApprovalsNodeGet(deps),
		"exec.approvals.node.set":    execApprovalsNodeSet(deps),
	}
}

func execApprovalRequest(deps ApprovalDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID                  string            `json:"id,omitempty"`
			Command             string            `json:"command"`
			CommandArgv         []string          `json:"commandArgv,omitempty"`
			Env                 map[string]string `json:"env,omitempty"`
			Cwd                 string            `json:"cwd,omitempty"`
			SystemRunPlan       any               `json:"systemRunPlan,omitempty"`
			NodeID              string            `json:"nodeId,omitempty"`
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
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"status":      "accepted",
				"id":          created.ID,
				"createdAtMs": created.CreatedAtMs,
				"expiresAtMs": created.ExpiresAtMs,
			})
			return resp
		}

		resp := protocol.MustResponseOK(req.ID, created)
		return resp
	}
}

func execApprovalWaitDecision(deps ApprovalDeps) rpcutil.HandlerFunc {
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
			resp := protocol.MustResponseOK(req.ID, existing)
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
			resp := protocol.MustResponseOK(req.ID, result)
			return resp
		case <-ctx.Done():
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrAgentTimeout, "wait for decision timed out"))
		}
	}
}

func execApprovalResolve(deps ApprovalDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func execApprovalsGet(deps ApprovalDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot := deps.Store.GetGlobalSnapshot()
		if snapshot == nil {
			snapshot = &approval.Snapshot{
				File:     approval.ApprovalsFile{Version: 1},
				LoadedAt: 0,
			}
		}
		resp := protocol.MustResponseOK(req.ID, snapshot)
		return resp
	}
}

func execApprovalsSet(deps ApprovalDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func execApprovalsNodeGet(deps ApprovalDeps) rpcutil.HandlerFunc {
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
		resp := protocol.MustResponseOK(req.ID, snapshot)
		return resp
	}
}

func execApprovalsNodeSet(deps ApprovalDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

// ---------------------------------------------------------------------------
// ACP (Agent Communication Protocol)
// ---------------------------------------------------------------------------

// ACPDeps holds dependencies for ACP RPC methods.
type ACPDeps struct {
	Registry     *autoreply.ACPRegistry
	Bindings     *autoreply.SessionBindingService
	Infra        *autoreply.SubagentInfraDeps
	Sessions     *session.Manager
	GatewaySubs  *events.GatewayEventSubscriptions
	BindingStore *autoreply.BindingStore
	Translator   *autoreply.ACPTranslator

	// SessionSendFn sends a message to a session, triggering an agent run.
	SessionSendFn func(sessionKey, message string) error

	// enabled tracks whether ACP write operations are active.
	enabled atomic.Bool
}

// IsEnabled returns whether ACP is currently enabled.
func (d *ACPDeps) IsEnabled() bool {
	return d.enabled.Load()
}

// SetEnabled sets the ACP enabled state.
func (d *ACPDeps) SetEnabled(v bool) {
	d.enabled.Store(v)
}

// ACPMethods returns all ACP RPC handlers.
func ACPMethods(deps *ACPDeps) map[string]rpcutil.HandlerFunc {
	if deps == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		// Read-only methods: always available.
		"acp.status":   acpStatus(deps),
		"acp.start":    acpStart(deps),
		"acp.stop":     acpStop(deps),
		"acp.list":     acpList(deps),
		"acp.bindings": acpBindings(deps),

		// Write methods: gated on enabled state.
		"acp.spawn":  acpSpawn(deps),
		"acp.kill":   acpKill(deps),
		"acp.send":   acpSend(deps),
		"acp.bind":   acpBind(deps),
		"acp.unbind": acpUnbind(deps),
	}
}

// requireEnabled returns an error response if ACP is disabled.
func requireEnabled(deps *ACPDeps, reqID string) *protocol.ResponseFrame {
	if !deps.enabled.Load() {
		return protocol.NewResponseError(reqID, protocol.NewError(
			protocol.ErrFeatureDisabled, "ACP is not enabled; call acp.start first"))
	}
	return nil
}

// --- acp.status ---

func acpStatus(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		agents := deps.Registry.List("")
		running := 0
		total := len(agents)
		for _, a := range agents {
			if a.Status == "running" || a.Status == "idle" {
				running++
			}
		}

		result := map[string]any{
			"enabled":      deps.enabled.Load(),
			"totalAgents":  total,
			"activeAgents": running,
		}

		if deps.Bindings != nil {
			result["bindings"] = len(deps.Bindings.Snapshot())
		}

		return protocol.MustResponseOK(req.ID, result)
	}
}

// --- acp.start ---

func acpStart(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		wasEnabled := deps.enabled.Swap(true)

		if deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: "acp:system",
				Reason:     "acp_started",
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"enabled":        true,
			"wasAlready":     wasEnabled,
			"startedAtEpoch": time.Now().UnixMilli(),
		})
	}
}

// --- acp.stop ---

func acpStop(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		wasEnabled := deps.enabled.Swap(false)

		// Persist bindings on stop.
		if deps.BindingStore != nil && deps.Bindings != nil {
			_ = deps.BindingStore.SyncFromService(deps.Bindings)
		}

		if deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: "acp:system",
				Reason:     "acp_stopped",
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"enabled":        false,
			"wasEnabled":     wasEnabled,
			"stoppedAtEpoch": time.Now().UnixMilli(),
		})
	}
}

// --- acp.list ---

func acpList(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ParentID string `json:"parentId"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		agents := deps.Registry.List(p.ParentID)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"agents": agents,
			"count":  len(agents),
		})
	}
}

// --- acp.spawn ---

func acpSpawn(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireEnabled(deps, req.ID); errResp != nil {
			return errResp
		}
		if deps.Infra == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "subagent infrastructure not available"))
		}

		var p struct {
			ParentSessionKey string `json:"parentSessionKey"`
			ParentAgentID    string `json:"parentAgentId"`
			Role             string `json:"role"`
			Model            string `json:"model"`
			Provider         string `json:"provider"`
			InitialMessage   string `json:"initialMessage"`
			MaxDepth         int    `json:"maxDepth"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Role == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "role is required"))
		}

		result := deps.Infra.SpawnSubagent(ctx, autoreply.SpawnSubagentParams{
			ParentSessionKey: p.ParentSessionKey,
			ParentAgentID:    p.ParentAgentID,
			Role:             p.Role,
			Model:            p.Model,
			Provider:         p.Provider,
			InitialMessage:   p.InitialMessage,
			MaxDepth:         p.MaxDepth,
		})
		if result.Error != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, result.Error.Error()))
		}

		// Send initial message if provided and send function is available.
		if p.InitialMessage != "" && deps.SessionSendFn != nil {
			_ = deps.SessionSendFn(result.SessionKey, p.InitialMessage)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"agentId":    result.AgentID,
			"sessionKey": result.SessionKey,
		})
	}
}

// --- acp.kill ---

func acpKill(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireEnabled(deps, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.AgentID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId is required"))
		}

		if deps.Infra != nil {
			if err := deps.Infra.KillSubagent(p.AgentID); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrNotFound, err.Error()))
			}
		} else {
			if !deps.Registry.Kill(p.AgentID) {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrNotFound, "agent not found: "+p.AgentID))
			}
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"killed":  true,
			"agentId": p.AgentID,
		})
	}
}

// --- acp.send ---

func acpSend(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireEnabled(deps, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Message == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "message is required"))
		}

		// Resolve target session key.
		targetKey := p.SessionKey
		if targetKey == "" && p.AgentID != "" {
			agent := deps.Registry.Get(p.AgentID)
			if agent == nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrNotFound, "agent not found: "+p.AgentID))
			}
			targetKey = agent.SessionKey
		}
		if targetKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId or sessionKey is required"))
		}

		if deps.SessionSendFn == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "session send not available"))
		}

		if err := deps.SessionSendFn(targetKey, p.Message); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "send failed: "+err.Error()))
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"sent":       true,
			"sessionKey": targetKey,
		})
	}
}

// --- acp.bind ---

func acpBind(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireEnabled(deps, req.ID); errResp != nil {
			return errResp
		}
		if deps.Bindings == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "binding service not available"))
		}

		var p struct {
			Channel          string `json:"channel"`
			AccountID        string `json:"accountId"`
			ConversationID   string `json:"conversationId"`
			TargetSessionKey string `json:"targetSessionKey"`
			BoundBy          string `json:"boundBy"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.TargetSessionKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "targetSessionKey is required"))
		}

		result := deps.Bindings.Bind(autoreply.SessionBindParams{
			Channel:          p.Channel,
			AccountID:        p.AccountID,
			ConversationID:   p.ConversationID,
			TargetSessionKey: p.TargetSessionKey,
			BoundBy:          p.BoundBy,
		})

		// Persist after bind.
		if deps.BindingStore != nil {
			_ = deps.BindingStore.SyncFromService(deps.Bindings)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"bindingId":      result.BindingID,
			"conversationId": result.ConversationID,
			"targetKey":      result.TargetKey,
		})
	}
}

// --- acp.unbind ---

func acpUnbind(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireEnabled(deps, req.ID); errResp != nil {
			return errResp
		}
		if deps.Bindings == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "binding service not available"))
		}

		var p struct {
			BindingID      string `json:"bindingId"`
			Channel        string `json:"channel"`
			AccountID      string `json:"accountId"`
			ConversationID string `json:"conversationId"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}

		// Resolve binding ID from conversation if not provided directly.
		bindingID := p.BindingID
		if bindingID == "" {
			entry := deps.Bindings.Resolve(p.Channel, p.AccountID, p.ConversationID)
			if entry == nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrNotFound, "no binding found for conversation"))
			}
			bindingID = entry.BindingID
		}

		if err := deps.Bindings.Unbind(bindingID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		// Persist after unbind.
		if deps.BindingStore != nil {
			_ = deps.BindingStore.SyncFromService(deps.Bindings)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"unbound":   true,
			"bindingId": bindingID,
		})
	}
}

// --- acp.bindings ---

func acpBindings(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Bindings == nil {
			return protocol.MustResponseOK(req.ID, map[string]any{
				"bindings": []any{},
				"count":    0,
			})
		}

		var p struct {
			SessionKey string `json:"sessionKey"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		if p.SessionKey != "" {
			entries := deps.Bindings.ListForSession(p.SessionKey)
			return protocol.MustResponseOK(req.ID, map[string]any{
				"bindings": entries,
				"count":    len(entries),
			})
		}

		// Return all bindings.
		all := deps.Bindings.Snapshot()
		return protocol.MustResponseOK(req.ID, map[string]any{
			"bindings": all,
			"count":    len(all),
		})
	}
}

// ---------------------------------------------------------------------------
// Cron Advanced
// ---------------------------------------------------------------------------

// CronAdvancedDeps holds the dependencies for advanced cron RPC methods.
type CronAdvancedDeps struct {
	Cron        *cron.Scheduler
	Broadcaster BroadcastFunc
}

// CronAdvancedMethods returns the advanced cron CRUD RPC handlers
// (wake, cron.status, cron.add, cron.update, cron.remove, cron.run, cron.runs).
// These complement the basic cron.list/get/unregister in the agent handler package.
func CronAdvancedMethods(deps CronAdvancedDeps) map[string]rpcutil.HandlerFunc {
	if deps.Cron == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"wake":        cronWake(deps),
		"cron.status": cronStatus(deps),
		"cron.add":    cronAdd(deps),
		"cron.update": cronUpdate(deps),
		"cron.remove": cronRemove(deps),
		"cron.run":    cronRun(deps),
		"cron.runs":   cronRuns(deps),
	}
}

func cronWake(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Mode string `json:"mode"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		nextHeartbeat := deps.Cron.NextRunAt()

		if deps.Broadcaster != nil {
			deps.Broadcaster("wake", map[string]any{
				"mode": p.Mode,
				"text": p.Text,
				"ts":   time.Now().UnixMilli(),
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nextHeartbeatAtMs": nextHeartbeat,
			"mode":              p.Mode,
		})
		return resp
	}
}

func cronStatus(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		running := deps.Cron.Running()
		nextRun := deps.Cron.NextRunAt()
		taskCount := len(deps.Cron.List())

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"running":     running,
			"nextRunAtMs": nextRun,
			"taskCount":   taskCount,
		})
		return resp
	}
}

func cronAdd(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Name       string `json:"name"`
			Schedule   string `json:"schedule"`
			Command    string `json:"command"`
			AgentID    string `json:"agentId,omitempty"`
			SessionKey string `json:"sessionKey,omitempty"`
			Enabled    *bool  `json:"enabled,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Name == "" || p.Schedule == "" || p.Command == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "name, schedule, and command are required"))
		}
		const maxCommandLen = 4096
		if len(p.Command) > maxCommandLen {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "command exceeds maximum length of 4096 characters"))
		}

		schedule, err := cron.ParseSchedule(p.Schedule)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid schedule: "+err.Error()))
		}

		// Use name as the task ID.
		schedule.Label = p.Name
		if regErr := deps.Cron.Register(ctx, p.Name, schedule, func(_ context.Context) error {
			// The actual cron command execution is handled by the task runner.
			return nil
		}); regErr != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, regErr.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "added", "id": p.Name})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"id":       p.Name,
			"name":     p.Name,
			"schedule": p.Schedule,
			"command":  p.Command,
		})
		return resp
	}
}

func cronUpdate(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string         `json:"id,omitempty"`
			JobID string         `json:"jobId,omitempty"`
			Patch map[string]any `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		if err := deps.Cron.Update(id, p.Patch); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "updated", "id": id})
		}

		status := deps.Cron.Get(id)
		resp := protocol.MustResponseOK(req.ID, status)
		return resp
	}
}

func cronRemove(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		removed := deps.Cron.Unregister(id)

		if deps.Broadcaster != nil && removed {
			deps.Broadcaster("cron.changed", map[string]any{"action": "removed", "id": id})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"removed": removed})
		return resp
	}
}

func cronRun(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
			Mode  string `json:"mode,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		result, err := deps.Cron.RunNow(ctx, id)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

func cronRuns(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Scope            string   `json:"scope,omitempty"`
			ID               string   `json:"id,omitempty"`
			JobID            string   `json:"jobId,omitempty"`
			Limit            int      `json:"limit,omitempty"`
			Offset           int      `json:"offset,omitempty"`
			Statuses         []string `json:"statuses,omitempty"`
			Status           string   `json:"status,omitempty"`
			DeliveryStatuses []string `json:"deliveryStatuses,omitempty"`
			Query            string   `json:"query,omitempty"`
			SortDir          string   `json:"sortDir,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		id := p.ID
		if id == "" {
			id = p.JobID
		}

		// Cap pagination to prevent pathologically large queries.
		limit := p.Limit
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
		offset := p.Offset
		if offset < 0 {
			offset = 0
		}

		runs := deps.Cron.Runs(id, limit, offset)

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"runs":  runs,
			"total": len(runs),
		})
		return resp
	}
}
