package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds the subsystems that built-in RPC methods need.
type Deps struct {
	Sessions         *session.Manager
	Channels         *channel.Registry
	ChannelLifecycle *channel.LifecycleManager
	GatewaySubs      *events.GatewayEventSubscriptions
	Version          string // Server version string (from --version flag).
}

// unmarshalParams safely unmarshals request params, handling nil/empty params.
func unmarshalParams(params json.RawMessage, v any) error {
	if len(params) == 0 {
		return errors.New("missing params")
	}
	return json.Unmarshal(params, v)
}

// maxKeyInErrorMsg is the maximum key length included in error messages.
// Prevents log inflation from pathologically large keys.
const maxKeyInErrorMsg = 128

// truncateForError truncates a string for safe inclusion in error messages.
func truncateForError(s string) string {
	if len(s) <= maxKeyInErrorMsg {
		return s
	}
	return s[:maxKeyInErrorMsg] + "..."
}

// registerCoreBuiltins registers the core Go-native RPC methods (health,
// sessions CRUD, channels, system) that remain in the rpc package.
// FFI-backed and domain-specific methods are now in handler/* subpackages.
func registerCoreBuiltins(d *Dispatcher, deps Deps) {
	d.Register("health.check", healthCheck(deps))
	d.Register("sessions.list", sessionsList(deps))
	d.Register("sessions.get", sessionsGet(deps))
	d.Register("sessions.delete", sessionsDelete(deps))
	d.Register("channels.list", channelsList(deps))
	d.Register("channels.get", channelsGet(deps))
	d.Register("channels.status", channelsStatus(deps))
	d.Register("system.info", systemInfo(deps))
	d.Register("channels.health", channelsHealth(deps))
}

func healthCheck(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"status":   "ok",
			"runtime":  "go",
			"ffi":      ffi.Available,
			"sessions": deps.Sessions.Count(),
			"channels": deps.Channels.List(),
		})
		return resp
	}
}

func sessionsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Sessions.List())
		return resp
	}
}

func sessionsGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		s := deps.Sessions.Get(p.Key)
		if s == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "session not found: "+truncateForError(p.Key)))
		}
		resp := protocol.MustResponseOK(req.ID, s)
		return resp
	}
}

func sessionsDelete(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key   string `json:"key"`
			Force bool   `json:"force"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		// Check if session is running (prevent accidental deletion).
		s := deps.Sessions.Get(p.Key)
		if s != nil && s.Status == session.StatusRunning && !p.Force {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, "session is currently running; use force=true to delete"))
		}
		found := deps.Sessions.Delete(p.Key)
		if found && deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: p.Key,
				Reason:     "deleted",
			})
		}
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"deleted": found})
		return resp
	}
}

func channelsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Channels.List())
		return resp
	}
}

func channelsGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		ch := deps.Channels.Get(p.ID)
		if ch == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "channel not found: "+truncateForError(p.ID)))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"id":           ch.ID(),
			"meta":         ch.Meta(),
			"capabilities": ch.Capabilities(),
			"status":       ch.Status(),
		})
		return resp
	}
}

func channelsStatus(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Channels.StatusAll())
		return resp
	}
}

func systemInfo(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		version := deps.Version
		if version == "" {
			version = "unknown"
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"runtime":      "go",
			"version":      version,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"arch":         runtime.GOARCH,
			"numCPU":       runtime.NumCPU(),
			"ffiAvailable": ffi.Available,
		})
		return resp
	}
}

// FFI-backed methods (protocol, security, media, parsing, memory, markdown,
// compaction, context engine, vega, ml) have been moved to handler/ffi/.

func channelsHealth(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.ChannelLifecycle == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"channels": []any{},
			})
			return resp
		}
		health := deps.ChannelLifecycle.HealthCheck()
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"channels": health,
		})
		return resp
	}
}

