package rpc

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HeartbeatState tracks the last heartbeat event and whether heartbeats are enabled.
type HeartbeatState struct {
	mu      sync.RWMutex
	enabled bool
	last    map[string]any
}

// NewHeartbeatState creates a new heartbeat state tracker with heartbeats enabled.
func NewHeartbeatState() *HeartbeatState {
	return &HeartbeatState{enabled: true}
}

// SetEnabled enables or disables heartbeats.
func (h *HeartbeatState) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

// Enabled returns whether heartbeats are currently enabled.
func (h *HeartbeatState) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// RecordHeartbeat stores the latest heartbeat event payload.
func (h *HeartbeatState) RecordHeartbeat(event map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = event
}

// Last returns the most recent heartbeat event, or nil if none recorded.
func (h *HeartbeatState) Last() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.last
}

// HeartbeatDeps holds dependencies for heartbeat RPC methods.
type HeartbeatDeps struct {
	State       *HeartbeatState
	Broadcaster BroadcastFunc
}

// RegisterHeartbeatMethods registers last-heartbeat and set-heartbeats RPC methods.
func RegisterHeartbeatMethods(d *Dispatcher, deps HeartbeatDeps) {
	d.Register("last-heartbeat", lastHeartbeat(deps))
	d.Register("set-heartbeats", setHeartbeats(deps))
}

func lastHeartbeat(deps HeartbeatDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		last := deps.State.Last()
		if last == nil {
			last = map[string]any{
				"ts":      time.Now().UnixMilli(),
				"enabled": deps.State.Enabled(),
			}
		}
		resp := protocol.MustResponseOK(req.ID, last)
		return resp
	}
}

func setHeartbeats(deps HeartbeatDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Enabled *bool `json:"enabled"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Enabled == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid set-heartbeats params: enabled (boolean) required"))
		}

		deps.State.SetEnabled(*p.Enabled)

		if deps.Broadcaster != nil {
			deps.Broadcaster("heartbeat.config", map[string]any{
				"enabled": *p.Enabled,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":      true,
			"enabled": *p.Enabled,
		})
		return resp
	}
}
