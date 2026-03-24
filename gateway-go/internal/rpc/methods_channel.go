package rpc

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChannelLifecycleDeps holds the dependencies for channel lifecycle RPC methods.
type ChannelLifecycleDeps struct {
	ChannelLifecycle *channel.LifecycleManager
	Hooks            *hooks.Registry
	Broadcaster      *events.Broadcaster
}

// RegisterChannelLifecycleMethods registers channel start/stop/restart RPC methods.
func RegisterChannelLifecycleMethods(d *Dispatcher, deps ChannelLifecycleDeps) {
	if deps.ChannelLifecycle == nil {
		return
	}

	d.Register("channels.start", channelStart(deps))
	d.Register("channels.stop", channelStop(deps))
	d.Register("channels.restart", channelRestart(deps))
}

// emitChannelLifecycleEvent fires the appropriate hook and broadcasts a
// channels.changed event after a successful channel operation.
func emitChannelLifecycleEvent(deps ChannelLifecycleDeps, id string, hookEvent hooks.Event, action string) {
	if deps.Hooks != nil {
		go deps.Hooks.Fire(context.Background(), hookEvent, map[string]string{
			"DENEB_CHANNEL_ID": id,
		})
	}
	if deps.Broadcaster != nil {
		deps.Broadcaster.Broadcast("channels.changed", map[string]any{
			"channelId": id,
			"action":    action,
			"ts":        time.Now().UnixMilli(),
		})
	}
}

func channelStart(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.StartChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel start failed: "+err.Error()))
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "started")
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"started": true, "id": p.ID})
		return resp
	}
}

func channelStop(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.StopChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel stop failed: "+err.Error()))
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelDisconnect, "stopped")
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"stopped": true, "id": p.ID})
		return resp
	}
}

func channelRestart(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.RestartChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel restart failed: "+err.Error()))
		}
		emitChannelLifecycleEvent(deps, p.ID, hooks.EventChannelConnect, "restarted")
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"restarted": true, "id": p.ID})
		return resp
	}
}
