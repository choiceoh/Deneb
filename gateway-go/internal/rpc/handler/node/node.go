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
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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
		"node.pair.request":              nodePairRequest(deps),
		"node.pair.list":                 nodePairList(deps),
		"node.pair.approve":              nodePairApprove(deps),
		"node.pair.reject":               nodePairReject(deps),
		"node.pair.verify":               nodePairVerify(deps),
		"node.list":                      nodeList(deps),
		"node.describe":                  nodeDescribe(deps),
		"node.rename":                    nodeRename(deps),
		"node.invoke":                    nodeInvoke(deps),
		"node.invoke.result":             nodeInvokeResult(deps),
		"node.canvas.capability.refresh": nodeCanvasCapabilityRefresh(deps),
		"node.pending.pull":              nodePendingPull(deps),
		"node.pending.ack":               nodePendingAck(deps),
		"node.pending.drain":             nodePendingDrain(deps),
		"node.pending.enqueue":           nodePendingEnqueue(deps),
		"node.event":                     nodeEvent(deps),
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
		return rpcutil.Bind[node.PairRequest](req, func(p node.PairRequest) (any, error) {
			if p.NodeID == "" {
				return nil, rpcerr.MissingParam("nodeId")
			}
			created := deps.Nodes.RequestPairing(p)
			if deps.Broadcaster != nil && !p.Silent {
				deps.Broadcaster("node.pair.requested", map[string]any{
					"requestId": created.RequestID,
					"nodeId":    created.NodeID,
				})
			}
			return map[string]any{"requestId": created.RequestID}, nil
		})
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
		return protocol.MustResponseOK(req.ID, map[string]any{
			"pending": pending,
			"paired":  paired,
		})
	}
}

func nodePairApprove(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		RequestID string `json:"requestId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.RequestID == "" {
				return nil, rpcerr.MissingParam("requestId")
			}
			paired, err := deps.Nodes.ApprovePairing(p.RequestID)
			if err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("node.pair.approved", map[string]any{"nodeId": paired.NodeID})
			}
			return map[string]any{"node": paired}, nil
		})
	}
}

func nodePairReject(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		RequestID string `json:"requestId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.RequestID == "" {
				return nil, rpcerr.MissingParam("requestId")
			}
			nodeID, err := deps.Nodes.RejectPairing(p.RequestID)
			if err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("node.pair.rejected", map[string]any{"nodeId": nodeID})
			}
			return map[string]any{"nodeId": nodeID}, nil
		})
	}
}

func nodePairVerify(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		NodeID string `json:"nodeId"`
		Token  string `json:"token"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.NodeID == "" || p.Token == "" {
				return nil, rpcerr.MissingParam("nodeId and token")
			}
			valid := deps.Nodes.VerifyToken(p.NodeID, p.Token)
			result := map[string]any{"valid": valid}
			if valid {
				result["nodeId"] = p.NodeID
			}
			return result, nil
		})
	}
}

func nodeList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		nodes := deps.Nodes.ListNodes()
		if nodes == nil {
			nodes = []node.NodeInfo{}
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"ts":    time.Now().UnixMilli(),
			"nodes": nodes,
		})
	}
}

func nodeDescribe(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		NodeID string `json:"nodeId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.NodeID == "" {
				return nil, rpcerr.MissingParam("nodeId")
			}
			info := deps.Nodes.DescribeNode(p.NodeID)
			if info == nil {
				return nil, rpcerr.NotFound("node")
			}
			return map[string]any{
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
			}, nil
		})
	}
}

func nodeRename(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		NodeID      string `json:"nodeId"`
		DisplayName string `json:"displayName"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.NodeID == "" || p.DisplayName == "" {
				return nil, rpcerr.MissingParam("nodeId and displayName")
			}
			if err := deps.Nodes.RenameNode(p.NodeID, p.DisplayName); err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			return map[string]any{
				"nodeId":      p.NodeID,
				"displayName": p.DisplayName,
			}, nil
		})
	}
}

func nodeInvoke(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[node.InvokeRequest](req, func(p node.InvokeRequest) (any, error) {
			if p.NodeID == "" || p.Command == "" || p.IdempotencyKey == "" {
				return nil, rpcerr.MissingParam("nodeId, command, and idempotencyKey")
			}

			// Check that the target node exists and is connected.
			info := deps.Nodes.DescribeNode(p.NodeID)
			if info == nil {
				return nil, rpcerr.NotFound("node")
			}
			if !info.Connected {
				return nil, rpcerr.Unavailable("node is not connected")
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
				deps.Broadcaster("node.pending.changed", map[string]any{"nodeId": p.NodeID})
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
					return nil, rpcerr.Unavailable("invoke returned nil result")
				}
				return result, nil
			case <-timeoutCtx.Done():
				return nil, rpcerr.New(protocol.ErrAgentTimeout, "node invoke timed out")
			}
		})
	}
}

func nodeInvokeResult(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		IdempotencyKey string `json:"idempotencyKey"`
		OK             bool   `json:"ok"`
		NodeID         string `json:"nodeId"`
		Command        string `json:"command"`
		Payload        any    `json:"payload,omitempty"`
		PayloadJSON    string `json:"payloadJSON,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.IdempotencyKey == "" {
				return nil, rpcerr.MissingParam("idempotencyKey")
			}
			result := &node.InvokeResult{
				OK:          p.OK,
				NodeID:      p.NodeID,
				Command:     p.Command,
				Payload:     p.Payload,
				PayloadJSON: p.PayloadJSON,
			}
			resolved := deps.Nodes.ResolveInvoke(p.IdempotencyKey, result)
			return map[string]bool{"resolved": resolved}, nil
		})
	}
}

func nodeCanvasCapabilityRefresh(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		hostURL := deps.CanvasHost
		if hostURL == "" {
			hostURL = "http://localhost:3100"
		}
		cap := deps.Nodes.RefreshCanvasCapability(hostURL)
		return protocol.MustResponseOK(req.ID, cap)
	}
}

func nodePendingPull(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// nodeId is optional; ignore unmarshal errors (params may be absent).
		var p struct {
			NodeID string `json:"nodeId,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		actions := deps.Nodes.PullActions(p.NodeID)
		if actions == nil {
			actions = make([]*node.PendingAction, 0)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":  p.NodeID,
			"actions": actions,
		})
	}
}

func nodePendingAck(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		NodeID string   `json:"nodeId,omitempty"`
		IDs    []string `json:"ids"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if len(p.IDs) == 0 {
				return nil, rpcerr.MissingParam("ids")
			}
			ackedIDs, remaining := deps.Nodes.AckActions(p.NodeID, p.IDs)
			return map[string]any{
				"nodeId":         p.NodeID,
				"ackedIds":       ackedIDs,
				"remainingCount": remaining,
			}, nil
		})
	}
}

func nodePendingDrain(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// maxItems is optional; ignore unmarshal errors (params may be absent).
		var p struct {
			NodeID   string `json:"nodeId,omitempty"`
			MaxItems int    `json:"maxItems,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		items, hasMore := deps.Nodes.DrainActions(p.NodeID, p.MaxItems)
		if items == nil {
			items = make([]*node.PendingAction, 0)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"nodeId":  p.NodeID,
			"items":   items,
			"hasMore": hasMore,
		})
	}
}

func nodePendingEnqueue(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		NodeID      string `json:"nodeId"`
		Type        string `json:"type"`
		Priority    string `json:"priority,omitempty"`
		ExpiresInMs int64  `json:"expiresInMs,omitempty"`
		Wake        bool   `json:"wake,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.NodeID == "" || p.Type == "" {
				return nil, rpcerr.MissingParam("nodeId and type")
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
				deps.Broadcaster("node.pending.changed", map[string]any{"nodeId": p.NodeID})
			}
			return map[string]any{
				"nodeId":        p.NodeID,
				"queued":        queued,
				"wakeTriggered": p.Wake,
			}, nil
		})
	}
}

func nodeEvent(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Event       string `json:"event"`
		Payload     any    `json:"payload,omitempty"`
		PayloadJSON string `json:"payloadJSON,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.Event == "" {
				return nil, rpcerr.MissingParam("event")
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("node.event."+p.Event, p.Payload)
			}
			return map[string]bool{"ok": true}, nil
		})
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
		return protocol.MustResponseOK(req.ID, map[string]any{"pairs": pairs})
	}
}

func devicePairApprove(deps DeviceDeps) rpcutil.HandlerFunc {
	type params struct {
		RequestID string `json:"requestId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.RequestID == "" {
				return nil, rpcerr.MissingParam("requestId")
			}
			dev, err := deps.Devices.Approve(p.RequestID)
			if err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("device.pair.approved", map[string]any{"deviceId": dev.DeviceID})
			}
			return map[string]any{"device": dev}, nil
		})
	}
}

func devicePairReject(deps DeviceDeps) rpcutil.HandlerFunc {
	type params struct {
		RequestID string `json:"requestId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.RequestID == "" {
				return nil, rpcerr.MissingParam("requestId")
			}
			if err := deps.Devices.Reject(p.RequestID); err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			return map[string]bool{"ok": true}, nil
		})
	}
}

func devicePairRemove(deps DeviceDeps) rpcutil.HandlerFunc {
	type params struct {
		DeviceID string `json:"deviceId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.DeviceID == "" {
				return nil, rpcerr.MissingParam("deviceId")
			}
			if err := deps.Devices.Remove(p.DeviceID); err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("device.pair.removed", map[string]any{"deviceId": p.DeviceID})
			}
			return map[string]bool{"ok": true}, nil
		})
	}
}

func deviceTokenRotate(deps DeviceDeps) rpcutil.HandlerFunc {
	type params struct {
		DeviceID string `json:"deviceId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.DeviceID == "" {
				return nil, rpcerr.MissingParam("deviceId")
			}
			newToken, err := deps.Devices.RotateToken(p.DeviceID)
			if err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			return map[string]any{
				"deviceId": p.DeviceID,
				"token":    newToken,
			}, nil
		})
	}
}

func deviceTokenRevoke(deps DeviceDeps) rpcutil.HandlerFunc {
	type params struct {
		DeviceID string `json:"deviceId"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.DeviceID == "" {
				return nil, rpcerr.MissingParam("deviceId")
			}
			if err := deps.Devices.RevokeToken(p.DeviceID); err != nil {
				return nil, rpcerr.NotFound(err.Error())
			}
			if deps.Broadcaster != nil {
				deps.Broadcaster("device.token.revoked", map[string]any{"deviceId": p.DeviceID})
			}
			return map[string]bool{"ok": true}, nil
		})
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
