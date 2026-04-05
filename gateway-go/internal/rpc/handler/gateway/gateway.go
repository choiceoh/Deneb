// Package gateway provides core gateway/runtime RPC handlers that depend on
// live server state (uptime, daemon status, event broadcast, and runtime
// config). These handlers were extracted from server wiring code.
package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps defines dependencies for gateway runtime handlers.
type Deps struct {
	Version         string
	StartedAt       time.Time
	RustFFI         bool
	ChannelsStatus  func() any
	SessionCount    func() int
	ConnectionCount func() int64
	LastHeartbeatMs func() int64
	Broadcast       func(event string, payload any) (int, []error)
	Models          func() any
	RuntimeConfig   func() map[string]any
	DaemonStatus    func() (any, bool)
	AgentActiveRuns func() int
	AgentCacheSize  func() int
	CurrentModel    func() string
}

// RuntimeMethods returns the health/status/runtime handler map.
//
// Note: Several methods here are intentionally overwritten by dedicated handler
// packages registered later in registerEarlyMethods (presence, system, provider).
// Only health, status, and daemon.status are unique to this handler; the rest
// serve as safe fallbacks that fire before the dedicated handlers register.
func RuntimeMethods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"health":               health(deps),
		"status":               status(deps),
		"gateway.identity.get": identity(deps),
		"last-heartbeat":       lastHeartbeat(deps),
		"set-heartbeats":       setHeartbeats(),
		"system-presence":      systemPresence(deps),
		"system-event":         systemEvent(deps),
		"models.list":          modelsList(deps),
		"config.get":           configGet(deps),
		"daemon.status":        daemonStatus(deps),
	}
}

func health(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		result := map[string]any{
			"status": "ok",
			"uptime": time.Since(deps.StartedAt).Milliseconds(),
		}
		if deps.CurrentModel != nil {
			if m := deps.CurrentModel(); m != "" {
				result["model"] = m
			}
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

func status(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		channels := any(map[string]any{})
		if deps.ChannelsStatus != nil {
			channels = deps.ChannelsStatus()
		}
		sessions := 0
		if deps.SessionCount != nil {
			sessions = deps.SessionCount()
		}
		var connections int64
		if deps.ConnectionCount != nil {
			connections = deps.ConnectionCount()
		}
		agentStats := map[string]int{}
		if deps.AgentActiveRuns != nil {
			agentStats["activeRuns"] = deps.AgentActiveRuns()
		}
		if deps.AgentCacheSize != nil {
			agentStats["cacheSize"] = deps.AgentCacheSize()
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"version":     deps.Version,
			"channels":    channels,
			"sessions":    sessions,
			"connections": connections,
			"agents":      agentStats,
		})
	}
}

func identity(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, map[string]any{
			"version": deps.Version,
			"runtime": "go",
			"uptime":  time.Since(deps.StartedAt).Milliseconds(),
			"rustFFI": deps.RustFFI,
		})
	}
}

func lastHeartbeat(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var ts int64
		if deps.LastHeartbeatMs != nil {
			ts = deps.LastHeartbeatMs()
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"lastHeartbeatMs": ts,
		})
	}
}

func setHeartbeats() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, map[string]bool{"ok": true})
	}
}

func systemPresence(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var payload any
		if len(req.Params) > 0 {
			var p struct {
				Payload any `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrInvalidRequest, "invalid params"))
			}
			payload = p.Payload
		}
		if deps.Broadcast == nil {
			return rpcutil.RespondOK(req.ID, map[string]int{"sent": 0})
		}
		sent, _ := deps.Broadcast("presence", payload)
		return rpcutil.RespondOK(req.ID, map[string]int{"sent": sent})
	}
}

func systemEvent(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		if deps.Broadcast == nil {
			return rpcutil.RespondOK(req.ID, map[string]int{"sent": 0})
		}
		sent, _ := deps.Broadcast(p.Event, p.Payload)
		return rpcutil.RespondOK(req.ID, map[string]int{"sent": sent})
	}
}

func modelsList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		models := []any{}
		if deps.Models != nil {
			if provided := deps.Models(); provided != nil {
				return rpcutil.RespondOK(req.ID, map[string]any{"models": provided})
			}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"models": models})
	}
}

func configGet(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.RuntimeConfig == nil {
			return rpcutil.RespondOK(req.ID, map[string]string{"status": "not_loaded"})
		}
		cfg := deps.RuntimeConfig()
		if cfg == nil {
			return rpcutil.RespondOK(req.ID, map[string]string{"status": "not_loaded"})
		}
		return rpcutil.RespondOK(req.ID, cfg)
	}
}

func daemonStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.DaemonStatus == nil {
			return rpcutil.RespondOK(req.ID, map[string]string{"state": "not_configured"})
		}
		status, ok := deps.DaemonStatus()
		if !ok {
			return rpcutil.RespondOK(req.ID, map[string]string{"state": "not_configured"})
		}
		return rpcutil.RespondOK(req.ID, status)
	}
}
