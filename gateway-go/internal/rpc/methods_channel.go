package rpc

import (
	"context"
	"encoding/json"
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

func channelStart(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.StartChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel start failed: "+err.Error()))
		}
		// Fire hook and broadcast.
		if deps.Hooks != nil {
			go deps.Hooks.Fire(context.Background(), hooks.EventChannelConnect, map[string]string{
				"DENEB_CHANNEL_ID": p.ID,
			})
		}
		if deps.Broadcaster != nil {
			deps.Broadcaster.Broadcast("channels.changed", map[string]any{
				"channelId": p.ID,
				"action":    "started",
				"ts":        time.Now().UnixMilli(),
			})
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"started": true, "id": p.ID})
		return resp
	}
}

func channelStop(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.StopChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel stop failed: "+err.Error()))
		}
		if deps.Hooks != nil {
			go deps.Hooks.Fire(context.Background(), hooks.EventChannelDisconnect, map[string]string{
				"DENEB_CHANNEL_ID": p.ID,
			})
		}
		if deps.Broadcaster != nil {
			deps.Broadcaster.Broadcast("channels.changed", map[string]any{
				"channelId": p.ID,
				"action":    "stopped",
				"ts":        time.Now().UnixMilli(),
			})
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"stopped": true, "id": p.ID})
		return resp
	}
}

func channelRestart(deps ChannelLifecycleDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.ChannelLifecycle.RestartChannel(ctx, p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "channel restart failed: "+err.Error()))
		}
		if deps.Hooks != nil {
			go deps.Hooks.Fire(context.Background(), hooks.EventChannelConnect, map[string]string{
				"DENEB_CHANNEL_ID": p.ID,
			})
		}
		if deps.Broadcaster != nil {
			deps.Broadcaster.Broadcast("channels.changed", map[string]any{
				"channelId": p.ID,
				"action":    "restarted",
				"ts":        time.Now().UnixMilli(),
			})
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"restarted": true, "id": p.ID})
		return resp
	}
}
