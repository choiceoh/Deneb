// Package agent provides RPC handlers for agent management, sessions,
// process/cron/hooks orchestration, and agents CRUD.
package agent

import (
	"context"

	agentpkg "github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/core/coresecurity"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ExtendedDeps holds the dependencies for extended RPC methods:
// agent.status, sessions.create, sessions.lifecycle, plus
// process and cron management.
type ExtendedDeps struct {
	Sessions       *session.Manager
	TelegramPlugin *telegram.Plugin
	GatewaySubs    *events.GatewayEventSubscriptions
	Processes      *process.Manager
	CronService    *cron.Service
	InternalHooks  *hooks.InternalRegistry
	Broadcaster    rpcutil.BroadcastFunc
}

// AgentsDeps holds the dependencies for agents CRUD RPC methods.
type AgentsDeps struct {
	Agents      *agentpkg.Store
	Broadcaster rpcutil.BroadcastFunc
}

// ExtendedMethods returns the extended agent/session/process/cron/hooks handlers.
func ExtendedMethods(deps ExtendedDeps) map[string]rpcutil.HandlerFunc {
	m := make(map[string]rpcutil.HandlerFunc)

	// Process management.
	if deps.Processes != nil {
		m["process.exec"] = processExec(deps)
		m["process.kill"] = processKill(deps)
		m["process.get"] = processGet(deps)
		m["process.list"] = processList(deps)
	}

	// Cron scheduling (routed through cron.Service).
	if deps.CronService != nil {
		m["cron.list"] = cronList(deps)
		m["cron.get"] = cronGet(deps)
		m["cron.unregister"] = cronUnregister(deps)
	}

	// Agent methods.
	m["agent.status"] = agentStatus(deps)
	m["sessions.create"] = sessionsCreate(deps)
	m["sessions.lifecycle"] = sessionsLifecycle(deps)

	return m
}

// CRUDMethods returns the agents.* CRUD handlers.
func CRUDMethods(deps AgentsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Agents == nil {
		return nil
	}

	return map[string]rpcutil.HandlerFunc{
		"agents.list":       agentsList(deps),
		"agents.create":     agentsCreate(deps),
		"agents.update":     agentsUpdate(deps),
		"agents.delete":     agentsDelete(deps),
		"agents.files.list": agentsFilesList(deps),
		"agents.files.get":  agentsFilesGet(deps),
		"agents.files.set":  agentsFilesSet(deps),
	}
}

// --- Process methods ---

func processExec(deps ExtendedDeps) rpcutil.HandlerFunc {
	return rpcutil.BindHandlerCtx[process.ExecRequest](func(ctx context.Context, p process.ExecRequest) (any, error) {
		if p.Command == "" {
			return nil, rpcerr.MissingParam("command")
		}
		result := deps.Processes.Execute(ctx, p)

		// Broadcast process completion event to subscribers.
		if deps.Broadcaster != nil && result != nil {
			deps.Broadcaster("process.completed", map[string]any{
				"id":       result.ID,
				"status":   result.Status,
				"exitCode": result.ExitCode,
				"ms":       result.RuntimeMs,
			})
		}

		return result, nil
	})
}

func processKill(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if err := deps.Processes.Kill(p.ID); err != nil {
			return nil, rpcerr.NotFound("process")
		}
		return map[string]bool{"killed": true}, nil
	})
}

func processGet(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		tracked := deps.Processes.Get(p.ID)
		if tracked == nil {
			return nil, rpcerr.NotFound("process")
		}
		return tracked, nil
	})
}

func processList(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Processes.List())
	}
}

// --- Cron methods (routed through cron.Service) ---

func cronList(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		jobs, err := deps.CronService.List(&cron.ListOptions{IncludeDisabled: true})
		if err != nil {
			return rpcerr.Wrap(protocol.ErrUnavailable, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, jobs)
	}
}

func cronGet(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		job := deps.CronService.Job(p.ID)
		if job == nil {
			return nil, rpcerr.NotFound("cron job")
		}
		return job, nil
	})
}

func cronUnregister(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		err := deps.CronService.Remove(p.ID)
		return map[string]bool{"removed": err == nil}, nil
	})
}

// --- Agent/session methods ---

func agentStatus(deps ExtendedDeps) rpcutil.HandlerFunc {
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
			"channels": func() []string {
				if deps.TelegramPlugin != nil {
					return []string{"telegram"}
				}
				return nil
			}(),
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

		if deps.CronService != nil {
			result["cronTasks"] = deps.CronService.Status().TaskCount
		}

		return rpcutil.RespondOK(req.ID, result)
	}
}

func sessionsCreate(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		Key  string `json:"key"`
		Kind string `json:"kind"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Key == "" {
			return nil, rpcerr.MissingParam("key")
		}
		if err := coresecurity.ValidateSessionKey(p.Key); err != nil {
			return nil, rpcerr.New(protocol.ErrValidationFailed, "invalid session key").
				WithSession(p.Key)
		}

		kind := session.Kind(protocol.ParseSessionKind(p.Kind))
		s := deps.Sessions.Create(p.Key, kind)
		if deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: p.Key,
				Reason:     "created",
			})
		}
		return s, nil
	})
}

func sessionsLifecycle(deps ExtendedDeps) rpcutil.HandlerFunc {
	type params struct {
		Key        string `json:"key"`
		Phase      string `json:"phase"`
		Ts         int64  `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
		StopReason string `json:"stopReason,omitempty"`
		Aborted    bool   `json:"aborted,omitempty"`
		StartedAt  *int64 `json:"startedAt,omitempty"`
		EndedAt    *int64 `json:"endedAt,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Key == "" || p.Phase == "" {
			return nil, rpcerr.MissingParam("key and phase")
		}
		if err := coresecurity.ValidateSessionKey(p.Key); err != nil {
			return nil, rpcerr.New(protocol.ErrValidationFailed, "invalid session key").
				WithSession(p.Key)
		}

		event := session.LifecycleEvent{
			Phase:      session.LifecyclePhase(p.Phase),
			Ts:         p.Ts,
			StopReason: p.StopReason,
			Aborted:    p.Aborted,
			StartedAt:  p.StartedAt,
			EndedAt:    p.EndedAt,
		}
		s := deps.Sessions.ApplyLifecycleEvent(p.Key, event)

		if deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: p.Key,
				Reason:     p.Phase,
			})
		}

		// Fire session lifecycle internal hooks with panic recovery.
		var hookEvent hooks.Event
		switch session.LifecyclePhase(p.Phase) {
		case session.PhaseStart:
			hookEvent = hooks.EventSessionStart
		case session.PhaseEnd, session.PhaseError:
			hookEvent = hooks.EventSessionEnd
		}
		if hookEvent != "" && deps.InternalHooks != nil {
			evt := hookEvent
			key := p.Key
			phase := p.Phase
			env := map[string]string{
				"DENEB_SESSION_KEY": key,
				"DENEB_PHASE":       phase,
			}
			go func() { //nolint:gosec // G118 — intentionally detached from request context for fire-and-forget hook
				defer func() { recover() }() //nolint:errcheck // fire-and-forget panic recovery
				deps.InternalHooks.TriggerFromEvent(context.Background(), evt, key, env)
			}()
		}

		return s, nil
	})
}

// --- Agents CRUD methods ---

func agentsList(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		agents := deps.Agents.List()
		if agents == nil {
			agents = make([]*agentpkg.Agent, 0)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"agents": agents})
	}
}

func agentsCreate(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID      string            `json:"agentId,omitempty"`
		Name         string            `json:"name,omitempty"`
		Description  string            `json:"description,omitempty"`
		Model        string            `json:"model,omitempty"`
		SystemPrompt string            `json:"systemPrompt,omitempty"`
		Tools        []string          `json:"tools,omitempty"`
		Metadata     map[string]string `json:"metadata,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		created := deps.Agents.Create(agentpkg.CreateParams{
			AgentID:      p.AgentID,
			Name:         p.Name,
			Description:  p.Description,
			Model:        p.Model,
			SystemPrompt: p.SystemPrompt,
			Tools:        p.Tools,
			Metadata:     p.Metadata,
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "created",
				"agentId": created.AgentID,
			})
		}

		return map[string]any{"agent": created}, nil
	})
}

func agentsUpdate(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID string         `json:"agentId"`
		Patch   map[string]any `json:"patch"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.AgentID == "" {
			return nil, rpcerr.MissingParam("agentId")
		}

		updated, err := deps.Agents.Update(p.AgentID, p.Patch)
		if err != nil {
			return nil, rpcerr.NotFound("agent").WithAgent(p.AgentID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "updated",
				"agentId": p.AgentID,
			})
		}

		return map[string]any{"agent": updated}, nil
	})
}

func agentsDelete(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID string `json:"agentId"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.AgentID == "" {
			return nil, rpcerr.MissingParam("agentId")
		}

		removed := deps.Agents.Delete(p.AgentID)
		if !removed {
			return nil, rpcerr.NotFound("agent").WithAgent(p.AgentID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "deleted",
				"agentId": p.AgentID,
			})
		}

		return map[string]bool{"removed": true}, nil
	})
}

func agentsFilesList(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID string `json:"agentId"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.AgentID == "" {
			return nil, rpcerr.MissingParam("agentId")
		}

		files, err := deps.Agents.ListFiles(p.AgentID)
		if err != nil {
			return nil, rpcerr.NotFound("agent").WithAgent(p.AgentID)
		}
		if files == nil {
			files = make([]*agentpkg.FileEntry, 0)
		}

		return map[string]any{"files": files}, nil
	})
}

func agentsFilesGet(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.AgentID == "" || p.Name == "" {
			return nil, rpcerr.MissingParam("agentId and name")
		}

		file, err := deps.Agents.File(p.AgentID, p.Name)
		if err != nil {
			return nil, rpcerr.NotFound("agent file").WithAgent(p.AgentID)
		}

		return file, nil
	})
}

func agentsFilesSet(deps AgentsDeps) rpcutil.HandlerFunc {
	type params struct {
		AgentID       string `json:"agentId"`
		Name          string `json:"name"`
		ContentBase64 string `json:"contentBase64,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.AgentID == "" || p.Name == "" {
			return nil, rpcerr.MissingParam("agentId and name")
		}

		file, err := deps.Agents.SetFile(p.AgentID, p.Name, p.ContentBase64)
		if err != nil {
			return nil, rpcerr.NotFound("agent").WithAgent(p.AgentID)
		}

		return file, nil
	})
}
