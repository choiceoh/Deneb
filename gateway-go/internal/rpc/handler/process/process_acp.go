package process

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// ACP (Agent Communication Protocol)
// ---------------------------------------------------------------------------

// ACPDeps holds dependencies for ACP RPC methods.
type ACPDeps struct {
	Registry     *acp.ACPRegistry
	Bindings     *acp.SessionBindingService
	Infra        *acp.SubagentInfraDeps
	Sessions     *session.Manager
	GatewaySubs  *events.GatewayEventSubscriptions
	BindingStore *acp.BindingStore
	Translator   *acp.ACPTranslator

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

		result := deps.Infra.SpawnSubagent(ctx, acp.SpawnSubagentParams{
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

		result := deps.Bindings.Bind(acp.SessionBindParams{
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
