package rpc

import (
	"context"
	"fmt"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	handlerffi "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/ffi"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds the subsystems that built-in RPC methods need.
type Deps struct {
	Sessions      *session.Manager
	SnapshotStore *telegram.SnapshotStore
	GatewaySubs   *events.GatewayEventSubscriptions
	Version       string // Server version string (from --version flag).
	// TelegramPlugin is set after channel wiring; may be nil in tests.
	TelegramPlugin *telegram.Plugin
}

// MethodModule is a domain-scoped RPC registration unit.
// Each module is responsible for registering its own methods.
type MethodModule interface {
	Register(*Dispatcher)
}

type namedMethodModule struct {
	name   string
	module MethodModule
}

// registerCoreBuiltins registers the core Go-native RPC methods via
// per-domain modules (health/session/channel/system).
// Duplicate method names are rejected during boot-time validation.
func registerCoreBuiltins(d *Dispatcher, deps Deps) error {
	modules := []namedMethodModule{
		{name: "core.health", module: healthModule{deps: deps}},
		{name: "core.session", module: sessionModule{deps: deps}},
		{name: "core.telegram", module: telegramModule{deps: deps}},
		{name: "core.system", module: systemModule{deps: deps}},
	}
	d.beginRegistryValidation()
	for _, m := range modules {
		d.setRegistryModule(m.name)
		m.module.Register(d)
	}
	if err := d.endRegistryValidation(); err != nil {
		return fmt.Errorf("core rpc module registration failed: %w", err)
	}
	return nil
}

type healthModule struct{ deps Deps }

func (m healthModule) Register(d *Dispatcher) {
	d.Register("health.check", healthCheck(m.deps))
}

type sessionModule struct{ deps Deps }

func (m sessionModule) Register(d *Dispatcher) {
	d.Register("sessions.list", sessionsList(m.deps))
	d.Register("sessions.get", sessionsGet(m.deps))
	d.Register("sessions.delete", sessionsDelete(m.deps))
}

type telegramModule struct{ deps Deps }

func (m telegramModule) Register(d *Dispatcher) {
	d.Register("telegram.list", telegramList(m.deps))
	d.Register("telegram.get", telegramGet(m.deps))
	d.Register("telegram.status", telegramStatus(m.deps))
	d.Register("telegram.health", telegramHealth(m.deps))
}

type systemModule struct{ deps Deps }

func (m systemModule) Register(d *Dispatcher) {
	d.Register("system.info", systemInfo(m.deps))
}

func healthCheck(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var channels []string
		if deps.TelegramPlugin != nil {
			channels = []string{"telegram"}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"status":   "ok",
			"runtime":  "go",
			"ffi":      ffi.Available,
			"sessions": deps.Sessions.Count(),
			"channels": channels,
		})
	}
}

func sessionsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Sessions.List())
	}
}

func sessionsGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Key string `json:"key"`
		}](req)
		if errResp != nil {
			return errResp
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}
		s := deps.Sessions.Get(key)
		if s == nil {
			return rpcerr.NotFound("session").
				WithSession(rpcutil.TruncateForError(key)).
				Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, s)
	}
}

func sessionsDelete(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Key   string `json:"key"`
			Force bool   `json:"force"`
		}](req)
		if errResp != nil {
			return errResp
		}
		key, errResp := rpcutil.RequireKey(req.ID, p.Key)
		if errResp != nil {
			return errResp
		}
		// Check if session is running (prevent accidental deletion).
		s := deps.Sessions.Get(key)
		if s != nil && s.Status == session.StatusRunning && !p.Force {
			return rpcerr.Conflict("session is currently running; use force=true to delete").
				WithSession(key).
				Response(req.ID)
		}
		found := deps.Sessions.Delete(key)
		if found && deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: key,
				Reason:     "deleted",
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]bool{"deleted": found})
	}
}

func telegramList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var channels []string
		if deps.TelegramPlugin != nil {
			channels = []string{"telegram"}
		}
		return rpcutil.RespondOK(req.ID, channels)
	}
}

func telegramGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID string `json:"id"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.ID != "telegram" || deps.TelegramPlugin == nil {
			return rpcerr.NotFound("channel").
				WithChannel(rpcutil.TruncateForError(p.ID)).
				Response(req.ID)
		}
		plug := deps.TelegramPlugin
		return rpcutil.RespondOK(req.ID, map[string]any{
			"id":           plug.ID(),
			"capabilities": plug.Capabilities(),
			"status":       plug.Status(),
		})
	}
}

func telegramStatus(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.SnapshotStore != nil {
			return rpcutil.RespondOK(req.ID, deps.SnapshotStore.Snapshot())
		}
		if deps.TelegramPlugin != nil {
			return rpcutil.RespondOK(req.ID, map[string]telegram.Status{
				"telegram": deps.TelegramPlugin.Status(),
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]telegram.Status{})
	}
}

func systemInfo(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		version := deps.Version
		if version == "" {
			version = "unknown"
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"runtime":      "go",
			"version":      version,
			"goVersion":    runtime.Version(),
			"os":           "linux",
			"arch":         runtime.GOARCH,
			"numCPU":       runtime.NumCPU(),
			"ffiAvailable": ffi.Available,
		})
	}
}

// FFI-backed methods (protocol, security, media, parsing, memory, markdown,
// compaction, context engine, vega, ml) have been moved to handler/ffi/.

func telegramHealth(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.TelegramPlugin == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"channels": []any{}})
		}
		status := deps.TelegramPlugin.Status()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"channels": []map[string]any{{
				"id":        "telegram",
				"connected": status.Connected,
				"error":     status.Error,
			}},
		})
	}
}

// RegisterBuiltinMethods registers the core Go-native RPC methods.
// Delegates to the FFI and skill handler packages for the bulk of methods,
// while keeping health/telegram/system in the rpc package.
func RegisterBuiltinMethods(d *Dispatcher, deps Deps) error {
	// Health, sessions CRUD, channels, system — kept in rpc package (methods.go).
	if err := registerCoreBuiltins(d, deps); err != nil {
		return err
	}

	// FFI-backed methods: protocol, security, media, parsing, memory, markdown,
	// compaction, context engine, ML.
	d.RegisterDomain(handlerffi.ProtocolMethods())
	d.RegisterDomain(handlerffi.SecurityMethods())
	d.RegisterDomain(handlerffi.MediaMethods())
	d.RegisterDomain(handlerffi.ParsingMethods())
	d.RegisterDomain(handlerffi.MemoryMethods())
	d.RegisterDomain(handlerffi.MarkdownMethods())
	d.RegisterDomain(handlerffi.CompactionMethods())
	d.RegisterDomain(handlerffi.ContextEngineMethods())

	// Tools catalog (static core tool definitions).
	d.RegisterDomain(handlerskill.CatalogMethods())
	return nil
}
