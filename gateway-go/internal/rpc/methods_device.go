package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// DeviceDeps holds the dependencies for device RPC methods.
type DeviceDeps struct {
	Devices     *device.Manager
	Broadcaster BroadcastFunc
}

// RegisterDeviceMethods registers all device.pair.* and device.token.* RPC methods.
func RegisterDeviceMethods(d *Dispatcher, deps DeviceDeps) {
	if deps.Devices == nil {
		return
	}

	d.Register("device.pair.list", devicePairList(deps))
	d.Register("device.pair.approve", devicePairApprove(deps))
	d.Register("device.pair.reject", devicePairReject(deps))
	d.Register("device.pair.remove", devicePairRemove(deps))
	d.Register("device.token.rotate", deviceTokenRotate(deps))
	d.Register("device.token.revoke", deviceTokenRevoke(deps))
}

func devicePairList(deps DeviceDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		pairs := deps.Devices.ListPairs()
		if pairs == nil {
			pairs = make([]*device.PairEntry, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{"pairs": pairs})
		return resp
	}
}

func devicePairApprove(deps DeviceDeps) HandlerFunc {
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

func devicePairReject(deps DeviceDeps) HandlerFunc {
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

func devicePairRemove(deps DeviceDeps) HandlerFunc {
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

func deviceTokenRotate(deps DeviceDeps) HandlerFunc {
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

func deviceTokenRevoke(deps DeviceDeps) HandlerFunc {
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
