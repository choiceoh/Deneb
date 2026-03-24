package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ExtendedDeps holds the additional subsystems for Phase 2 RPC methods.
type ExtendedDeps struct {
	Deps
	Processes *process.Manager
	Cron      *cron.Scheduler
	Hooks     *hooks.Registry
}

// RegisterExtendedMethods registers the Phase 2 RPC methods that handle
// agent processes, cron scheduling, hooks, and daemon management.
func RegisterExtendedMethods(d *Dispatcher, deps ExtendedDeps) {
	// Process management.
	if deps.Processes != nil {
		d.Register("process.exec", processExec(deps))
		d.Register("process.kill", processKill(deps))
		d.Register("process.get", processGet(deps))
		d.Register("process.list", processList(deps))
	}

	// Cron scheduling.
	if deps.Cron != nil {
		d.Register("cron.list", cronList(deps))
		d.Register("cron.get", cronGet(deps))
		d.Register("cron.unregister", cronUnregister(deps))
	}

	// Hook management.
	if deps.Hooks != nil {
		d.Register("hooks.list", hooksList(deps))
		d.Register("hooks.register", hooksRegister(deps))
		d.Register("hooks.unregister", hooksUnregister(deps))
		d.Register("hooks.fire", hooksFire(deps))
	}

	// Agent methods.
	d.Register("agent.status", agentStatus(deps))
	d.Register("sessions.create", sessionsCreate(deps))
	d.Register("sessions.lifecycle", sessionsLifecycle(deps))
}

// --- Process methods ---

func processExec(deps ExtendedDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p process.ExecRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid exec params: "+err.Error()))
		}
		if p.Command == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "command is required"))
		}
		result := deps.Processes.Execute(ctx, p)
		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

func processKill(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		if err := deps.Processes.Kill(p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"killed": true})
		return resp
	}
}

func processGet(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		tracked := deps.Processes.Get(p.ID)
		if tracked == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "process not found"))
		}
		resp, _ := protocol.NewResponseOK(req.ID, tracked)
		return resp
	}
}

func processList(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, deps.Processes.List())
		return resp
	}
}

// --- Cron methods ---

func cronList(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, deps.Cron.List())
		return resp
	}
}

func cronGet(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		status := deps.Cron.Get(p.ID)
		if status == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "cron task not found"))
		}
		resp, _ := protocol.NewResponseOK(req.ID, status)
		return resp
	}
}

func cronUnregister(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		found := deps.Cron.Unregister(p.ID)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"removed": found})
		return resp
	}
}

// --- Hook methods ---

func hooksList(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, deps.Hooks.List())
		return resp
	}
}

func hooksRegister(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var hook hooks.Hook
		if err := json.Unmarshal(req.Params, &hook); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid hook params: "+err.Error()))
		}
		if err := deps.Hooks.Register(hook); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"registered": true})
		return resp
	}
}

func hooksUnregister(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		found := deps.Hooks.Unregister(p.ID)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"removed": found})
		return resp
	}
}

func hooksFire(deps ExtendedDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event hooks.Event       `json:"event"`
			Env   map[string]string `json:"env,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		results := deps.Hooks.Fire(ctx, p.Event, p.Env)
		resp, _ := protocol.NewResponseOK(req.ID, results)
		return resp
	}
}

// --- Agent/session methods ---

func agentStatus(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		activeSessions := 0
		for _, s := range deps.Sessions.List() {
			if s.Status == session.StatusRunning {
				activeSessions++
			}
		}

		result := map[string]any{
			"activeSessions": activeSessions,
			"totalSessions":  deps.Sessions.Count(),
			"channels":       deps.Channels.List(),
		}

		if deps.Processes != nil {
			running := 0
			for _, p := range deps.Processes.List() {
				if p.Status == process.StatusRunning {
					running++
				}
			}
			result["activeProcesses"] = running
		}

		if deps.Cron != nil {
			result["cronTasks"] = len(deps.Cron.List())
		}

		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

func sessionsCreate(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key  string `json:"key"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		// Validate session key format (Rust FFI or Go fallback).
		if err := ffi.ValidateSessionKey(p.Key); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid session key"))
		}

		kind := session.KindDirect
		switch p.Kind {
		case "group":
			kind = session.KindGroup
		case "global":
			kind = session.KindGlobal
		case "unknown":
			kind = session.KindUnknown
		}
		s := deps.Sessions.Create(p.Key, kind)
		resp, _ := protocol.NewResponseOK(req.ID, s)
		return resp
	}
}

func sessionsLifecycle(deps ExtendedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key   string `json:"key"`
			Phase string `json:"phase"`
			Ts    int64  `json:"ts"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Key == "" || p.Phase == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key and phase are required"))
		}
		if err := ffi.ValidateSessionKey(p.Key); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid session key"))
		}

		event := session.LifecycleEvent{
			Phase: session.LifecyclePhase(p.Phase),
			Ts:    p.Ts,
		}
		s := deps.Sessions.ApplyLifecycleEvent(p.Key, event)
		resp, _ := protocol.NewResponseOK(req.ID, s)
		return resp
	}
}
