package process

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// ACP (Agent Communication Protocol)
// ---------------------------------------------------------------------------

// ACPDeps holds dependencies for ACP RPC methods.
// Pointer receiver required: enabled field is an atomic.Bool mutated by
// IsEnabled/SetEnabled, so ACPMethods takes *ACPDeps (not a value).
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

	// TranscriptLoader loads transcript history for a subagent session.
	// Returns (role, content) pairs. Wired from chat.TranscriptStore.
	TranscriptLoader func(sessionKey string, limit int) (roles []string, contents []string, err error)

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
		return rpcerr.New(protocol.ErrFeatureDisabled, "ACP is not enabled; call acp.start first").Response(reqID)
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

		return rpcutil.RespondOK(req.ID, result)
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

		return rpcutil.RespondOK(req.ID, map[string]any{
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

		return rpcutil.RespondOK(req.ID, map[string]any{
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
		return rpcutil.RespondOK(req.ID, map[string]any{
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
			return rpcerr.New(protocol.ErrDependencyFailed, "subagent infrastructure not available").Response(req.ID)
		}

		p, errResp := rpcutil.DecodeParams[struct {
			ParentSessionKey string `json:"parentSessionKey"`
			ParentAgentID    string `json:"parentAgentId"`
			Role             string `json:"role"`
			Model            string `json:"model"`
			Provider         string `json:"provider"`
			InitialMessage   string `json:"initialMessage"`
			MaxDepth         int    `json:"maxDepth"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Role == "" {
			return rpcerr.MissingParam("role").Response(req.ID)
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
			return rpcerr.Wrap(protocol.ErrConflict, result.Error).Response(req.ID)
		}

		// Send initial message if provided and send function is available.
		if p.InitialMessage != "" && deps.SessionSendFn != nil {
			_ = deps.SessionSendFn(result.SessionKey, p.InitialMessage)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
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
			return rpcerr.MissingParam("agentId").Response(req.ID)
		}

		if deps.Infra != nil {
			if err := deps.Infra.KillSubagent(p.AgentID); err != nil {
				return rpcerr.Wrap(protocol.ErrNotFound, err).Response(req.ID)
			}
		} else {
			if !deps.Registry.Kill(p.AgentID) {
				return rpcerr.Newf(protocol.ErrNotFound, "agent not found: %s", p.AgentID).Response(req.ID)
			}
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
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

		p, errResp := rpcutil.DecodeParams[struct {
			AgentID    string `json:"agentId"`
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Message == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}

		// Resolve target session key.
		targetKey := p.SessionKey
		if targetKey == "" && p.AgentID != "" {
			agent := deps.Registry.Get(p.AgentID)
			if agent == nil {
				return rpcerr.Newf(protocol.ErrNotFound, "agent not found: %s", p.AgentID).Response(req.ID)
			}
			targetKey = agent.SessionKey
		}
		if targetKey == "" {
			return rpcerr.New(protocol.ErrMissingParam, "agentId or sessionKey is required").Response(req.ID)
		}

		if deps.SessionSendFn == nil {
			return rpcerr.New(protocol.ErrDependencyFailed, "session send not available").Response(req.ID)
		}

		if err := deps.SessionSendFn(targetKey, p.Message); err != nil {
			return rpcerr.Newf(protocol.ErrDependencyFailed, "send failed: %v", err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
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
			return rpcerr.New(protocol.ErrDependencyFailed, "binding service not available").Response(req.ID)
		}

		p, errResp := rpcutil.DecodeParams[struct {
			Channel          string `json:"channel"`
			AccountID        string `json:"accountId"`
			ConversationID   string `json:"conversationId"`
			TargetSessionKey string `json:"targetSessionKey"`
			BoundBy          string `json:"boundBy"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.TargetSessionKey == "" {
			return rpcerr.MissingParam("targetSessionKey").Response(req.ID)
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

		return rpcutil.RespondOK(req.ID, map[string]any{
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
			return rpcerr.New(protocol.ErrDependencyFailed, "binding service not available").Response(req.ID)
		}

		p, errResp := rpcutil.DecodeParams[struct {
			BindingID      string `json:"bindingId"`
			Channel        string `json:"channel"`
			AccountID      string `json:"accountId"`
			ConversationID string `json:"conversationId"`
		}](req)
		if errResp != nil {
			return errResp
		}

		// Resolve binding ID from conversation if not provided directly.
		bindingID := p.BindingID
		if bindingID == "" {
			entry := deps.Bindings.Resolve(p.Channel, p.AccountID, p.ConversationID)
			if entry == nil {
				return rpcerr.New(protocol.ErrNotFound, "no binding found for conversation").Response(req.ID)
			}
			bindingID = entry.BindingID
		}

		if err := deps.Bindings.Unbind(bindingID); err != nil {
			return rpcerr.Wrap(protocol.ErrNotFound, err).Response(req.ID)
		}

		// Persist after unbind.
		if deps.BindingStore != nil {
			_ = deps.BindingStore.SyncFromService(deps.Bindings)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"unbound":   true,
			"bindingId": bindingID,
		})
	}
}

// --- acp.bindings ---

func acpBindings(deps *ACPDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Bindings == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
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
			return rpcutil.RespondOK(req.ID, map[string]any{
				"bindings": entries,
				"count":    len(entries),
			})
		}

		// Return all bindings.
		all := deps.Bindings.Snapshot()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"bindings": all,
			"count":    len(all),
		})
	}
}
