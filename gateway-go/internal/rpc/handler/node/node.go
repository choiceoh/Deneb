// Package node provides RPC handlers for node.* and device.* methods.
//
// Migrated from the flat internal/rpc package into a domain-based handler
// subpackage. All handler functions are self-contained closures that capture
// their respective Deps/DeviceDeps structs.
package node

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/node"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc broadcasts an event to all connected clients.
// Returns the number of clients notified and any errors encountered.
type BroadcastFunc func(event string, payload any) (int, []error)

// Deps holds the dependencies for node.* RPC methods.
type Deps struct {
	Nodes       *node.Manager
	Broadcaster BroadcastFunc
	CanvasHost  string // canvas host URL for capability refresh
}

// DeviceDeps holds the dependencies for device.* RPC methods.
type DeviceDeps struct {
	Devices     *device.Manager
	Broadcaster BroadcastFunc
}

// Methods returns all node.* RPC handlers keyed by method name.
// If Deps.Nodes is nil, an empty map is returned.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Nodes == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"node.pair.request":                nodePairRequest(deps),
		"node.pair.list":                   nodePairList(deps),
		"node.pair.approve":                nodePairApprove(deps),
		"node.pair.reject":                 nodePairReject(deps),
		"node.pair.verify":                 nodePairVerify(deps),
		"node.list":                        nodeList(deps),
		"node.describe":                    nodeDescribe(deps),
		"node.rename":                      nodeRename(deps),
		"node.invoke":                      nodeInvoke(deps),
		"node.invoke.result":               nodeInvokeResult(deps),
		"node.canvas.capability.refresh":   nodeCanvasCapabilityRefresh(deps),
		"node.pending.pull":                nodePendingPull(deps),
		"node.pending.ack":                 nodePendingAck(deps),
		"node.pending.drain":               nodePendingDrain(deps),
		"node.pending.enqueue":             nodePendingEnqueue(deps),
		"node.event":                       nodeEvent(deps),
	}
}

// DeviceMethods returns all device.* RPC handlers keyed by method name.
// If DeviceDeps.Devices is nil, an empty map is returned.
func DeviceMethods(deps DeviceDeps) map[string]rpcutil.HandlerFunc {
	if deps.Devices == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"device.pair.list":    devicePairList(deps),
		"device.pair.approve": devicePairApprove(deps),
		"device.pair.reject":  devicePairReject(deps),
		"device.pair.remove":  devicePairRemove(deps),
		"device.token.rotate": deviceTokenRotate(deps),
		"device.token.revoke": deviceTokenRevoke(deps),
	}
}

// ---------------------------------------------------------------------------
// node.* handlers
// ---------------------------------------------------------------------------

func nodePairRequest(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p node.PairRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.NodeID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId is required"))
		}

		created := deps.Nodes.RequestPairing(p)

		if deps.Broadcaster != nil && !p.Silent {
			deps.Broadcaster("node.pair.requested", map[string]any{
				"requestId": created.RequestID,
				"nodeId":    created.NodeID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"requestId": created.RequestID,
		})
		return resp
	}
}

func nodePairList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		pending, paired := deps.Nodes.ListPairRequests()
		if pending == nil {
			pending = make([]*node.PairRequest, 0)
		}
		if paired == nil {
			paired = make([]*node.PairedNode, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"pending": pending,
			"paired":  paired,
		})
		return resp
	}
}

func nodePairApprove(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RequestID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "requestId is required"))
		}

		paired, err := deps.Nodes.ApprovePairing(p.RequestID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("node.pair.approved", map[string]any{
				"nodeId": paired.NodeID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{"node": paired})
		return resp
	}
}

func nodePairReject(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RequestID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "requestId is required"))
		}

		nodeID, err := deps.Nodes.RejectPairing(p.RequestID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("node.pair.rejected", map[string]any{
				"nodeId": nodeID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{"nodeId": nodeID})
		return resp
	}
}

func nodePairVerify(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID string `json:"nodeId"`
			Token  string `json:"token"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.NodeID == "" || p.Token == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId and token are required"))
		}

		valid := deps.Nodes.VerifyToken(p.NodeID, p.Token)
		result := map[string]any{"valid": valid}
		if valid {
			result["nodeId"] = p.NodeID
		}
		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

func nodeList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		nodes := deps.Nodes.ListNodes()
		if nodes == nil {
			nodes = []node.NodeInfo{}
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ts":    time.Now().UnixMilli(),
			"nodes": nodes,
		})
		return resp
	}
}

func nodeDescribe(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID string `json:"nodeId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.NodeID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId is required"))
		}

		info := deps.Nodes.DescribeNode(p.NodeID)
		if info == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "node not found"))
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ts":              time.Now().UnixMilli(),
			"nodeId":          info.NodeID,
			"displayName":     info.DisplayName,
			"platform":        info.Platform,
			"version":         info.Version,
			"coreVersion":     info.CoreVersion,
			"uiVersion":       info.UIVersion,
			"deviceFamily":    info.DeviceFamily,
			"modelIdentifier": info.ModelIdentifier,
			"caps":            info.Caps,
			"commands":        info.Commands,
			"paired":          info.Paired,
			"connected":       info.Connected,
			"lastSeenAtMs":    info.LastSeenAtMs,
		})
		return resp
	}
}

func nodeRename(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID      string `json:"nodeId"`
			DisplayName string `json:"displayName"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.NodeID == "" || p.DisplayName == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId and displayName are required"))
		}

		if err := deps.Nodes.RenameNode(p.NodeID, p.DisplayName); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":      p.NodeID,
			"displayName": p.DisplayName,
		})
		return resp
	}
}

func nodeInvoke(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p node.InvokeRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.NodeID == "" || p.Command == "" || p.IdempotencyKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId, command, and idempotencyKey are required"))
		}

		// Check that the target node exists and is connected.
		info := deps.Nodes.DescribeNode(p.NodeID)
		if info == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "node not found"))
		}
		if !info.Connected {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "node is not connected"))
		}

		// Register a waiter for the result.
		resultCh := deps.Nodes.RegisterInvokeWaiter(p.IdempotencyKey)

		// Enqueue the invoke action for the node to pull.
		deps.Nodes.EnqueueAction(p.NodeID, node.PendingAction{
			Command:    p.Command,
			ParamsJSON: marshalJSON(p.Params),
			Type:       "invoke",
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("node.pending.changed", map[string]any{
				"nodeId": p.NodeID,
			})
		}

		// Wait for the result with timeout (capped at 5 minutes).
		timeout := 30 * time.Second
		if p.TimeoutMs > 0 {
			timeout = time.Duration(p.TimeoutMs) * time.Millisecond
			const maxTimeout = 5 * time.Minute
			if timeout > maxTimeout {
				timeout = maxTimeout
			}
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		select {
		case result := <-resultCh:
			if result == nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "invoke returned nil result"))
			}
			resp := protocol.MustResponseOK(req.ID, result)
			return resp
		case <-timeoutCtx.Done():
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrAgentTimeout, "node invoke timed out"))
		}
	}
}

func nodeInvokeResult(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			IdempotencyKey string `json:"idempotencyKey"`
			OK             bool   `json:"ok"`
			NodeID         string `json:"nodeId"`
			Command        string `json:"command"`
			Payload        any    `json:"payload,omitempty"`
			PayloadJSON    string `json:"payloadJSON,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.IdempotencyKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "idempotencyKey is required"))
		}

		result := &node.InvokeResult{
			OK:          p.OK,
			NodeID:      p.NodeID,
			Command:     p.Command,
			Payload:     p.Payload,
			PayloadJSON: p.PayloadJSON,
		}
		resolved := deps.Nodes.ResolveInvoke(p.IdempotencyKey, result)

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"resolved": resolved})
		return resp
	}
}

func nodeCanvasCapabilityRefresh(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		hostURL := deps.CanvasHost
		if hostURL == "" {
			hostURL = "http://localhost:3100"
		}
		cap := deps.Nodes.RefreshCanvasCapability(hostURL)
		resp := protocol.MustResponseOK(req.ID, cap)
		return resp
	}
}

func nodePendingPull(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// The nodeId is determined by the authenticated connection context.
		// For now, parse from params or use a placeholder.
		var p struct {
			NodeID string `json:"nodeId,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		actions := deps.Nodes.PullActions(p.NodeID)
		if actions == nil {
			actions = make([]*node.PendingAction, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":  p.NodeID,
			"actions": actions,
		})
		return resp
	}
}

func nodePendingAck(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID string   `json:"nodeId,omitempty"`
			IDs    []string `json:"ids"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if len(p.IDs) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "ids is required"))
		}

		ackedIDs, remaining := deps.Nodes.AckActions(p.NodeID, p.IDs)
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":         p.NodeID,
			"ackedIds":       ackedIDs,
			"remainingCount": remaining,
		})
		return resp
	}
}

func nodePendingDrain(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID   string `json:"nodeId,omitempty"`
			MaxItems int    `json:"maxItems,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		items, hasMore := deps.Nodes.DrainActions(p.NodeID, p.MaxItems)
		if items == nil {
			items = make([]*node.PendingAction, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":  p.NodeID,
			"items":   items,
			"hasMore": hasMore,
		})
		return resp
	}
}

func nodePendingEnqueue(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			NodeID      string `json:"nodeId"`
			Type        string `json:"type"`
			Priority    string `json:"priority,omitempty"`
			ExpiresInMs int64  `json:"expiresInMs,omitempty"`
			Wake        bool   `json:"wake,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.NodeID == "" || p.Type == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "nodeId and type are required"))
		}

		action := node.PendingAction{
			Command:  p.Type,
			Priority: p.Priority,
			Type:     p.Type,
		}
		if p.ExpiresInMs > 0 {
			action.ExpiresAtMs = time.Now().Add(time.Duration(p.ExpiresInMs) * time.Millisecond).UnixMilli()
		}

		queued := deps.Nodes.EnqueueAction(p.NodeID, action)

		if deps.Broadcaster != nil {
			deps.Broadcaster("node.pending.changed", map[string]any{
				"nodeId": p.NodeID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":        p.NodeID,
			"queued":        queued,
			"wakeTriggered": p.Wake,
		})
		return resp
	}
}

func nodeEvent(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event       string `json:"event"`
			Payload     any    `json:"payload,omitempty"`
			PayloadJSON string `json:"payloadJSON,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("node.event."+p.Event, p.Payload)
		}

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

// ---------------------------------------------------------------------------
// device.* handlers
// ---------------------------------------------------------------------------

func devicePairList(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		pairs := deps.Devices.ListPairs()
		if pairs == nil {
			pairs = make([]*device.PairEntry, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{"pairs": pairs})
		return resp
	}
}

func devicePairApprove(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RequestID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "requestId is required"))
		}

		dev, err := deps.Devices.Approve(p.RequestID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("device.pair.approved", map[string]any{
				"deviceId": dev.DeviceID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{"device": dev})
		return resp
	}
}

func devicePairReject(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.RequestID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "requestId is required"))
		}

		if err := deps.Devices.Reject(p.RequestID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func devicePairRemove(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			DeviceID string `json:"deviceId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.DeviceID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "deviceId is required"))
		}

		if err := deps.Devices.Remove(p.DeviceID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("device.pair.removed", map[string]any{
				"deviceId": p.DeviceID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

func deviceTokenRotate(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			DeviceID string `json:"deviceId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.DeviceID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "deviceId is required"))
		}

		newToken, err := deps.Devices.RotateToken(p.DeviceID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"deviceId": p.DeviceID,
			"token":    newToken,
		})
		return resp
	}
}

func deviceTokenRevoke(deps DeviceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			DeviceID string `json:"deviceId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.DeviceID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "deviceId is required"))
		}

		if err := deps.Devices.RevokeToken(p.DeviceID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("device.token.revoked", map[string]any{
				"deviceId": p.DeviceID,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// marshalJSON is a helper to marshal any value to a JSON string.
func marshalJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
