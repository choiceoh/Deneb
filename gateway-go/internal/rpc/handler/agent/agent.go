// Package agent provides RPC handlers for agent management, sessions,
// process/cron/hooks orchestration, agents CRUD, and autonomous mode.
package agent

import (
	"context"
	"encoding/json"

	agentpkg "github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc matches the event broadcast signature used by the gateway.
type BroadcastFunc func(event string, payload any) (int, []error)

// ExtendedDeps holds the dependencies for extended RPC methods:
// agent.status, sessions.create, sessions.lifecycle, plus
// process, cron, and hooks management.
type ExtendedDeps struct {
	Sessions         *session.Manager
	Channels         *channel.Registry
	ChannelLifecycle *channel.LifecycleManager
	GatewaySubs      *events.GatewayEventSubscriptions
	Processes        *process.Manager
	Cron             *cron.Scheduler
	Hooks            *hooks.Registry
	Broadcaster      *events.Broadcaster
}

// AgentsDeps holds the dependencies for agents CRUD RPC methods.
type AgentsDeps struct {
	Agents      *agentpkg.Store
	Broadcaster BroadcastFunc
}

// AutonomousDeps holds the dependencies for autonomous RPC methods.
type AutonomousDeps struct {
	Autonomous *autonomous.Service
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

	// Cron scheduling.
	if deps.Cron != nil {
		m["cron.list"] = cronList(deps)
		m["cron.get"] = cronGet(deps)
		m["cron.unregister"] = cronUnregister(deps)
	}

	// Hook management.
	if deps.Hooks != nil {
		m["hooks.list"] = hooksList(deps)
		m["hooks.register"] = hooksRegister(deps)
		m["hooks.unregister"] = hooksUnregister(deps)
		m["hooks.fire"] = hooksFire(deps)
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
		"agents.list":      agentsList(deps),
		"agents.create":    agentsCreate(deps),
		"agents.update":    agentsUpdate(deps),
		"agents.delete":    agentsDelete(deps),
		"agents.files.list": agentsFilesList(deps),
		"agents.files.get":  agentsFilesGet(deps),
		"agents.files.set":  agentsFilesSet(deps),
	}
}

// AutonomousMethods returns the autonomous.* handlers.
func AutonomousMethods(deps AutonomousDeps) map[string]rpcutil.HandlerFunc {
	if deps.Autonomous == nil {
		return nil
	}

	return map[string]rpcutil.HandlerFunc{
		"autonomous.status":       autonomousStatus(deps),
		"autonomous.goals.list":   autonomousGoalsList(deps),
		"autonomous.goals.add":    autonomousGoalsAdd(deps),
		"autonomous.goals.update": autonomousGoalsUpdate(deps),
		"autonomous.goals.remove": autonomousGoalsRemove(deps),
		"autonomous.cycle.run":    autonomousCycleRun(deps),
		"autonomous.cycle.stop":   autonomousCycleStop(deps),
		"autonomous.enable":       autonomousEnable(deps),
	}
}

// --- Process methods ---

func processExec(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p process.ExecRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Command == "" {
			return rpcerr.MissingParam("command").Response(req.ID)
		}
		result := deps.Processes.Execute(ctx, p)

		// Broadcast process completion event to subscribers.
		if deps.Broadcaster != nil && result != nil {
			deps.Broadcaster.Broadcast("process.completed", map[string]any{
				"id":       result.ID,
				"status":   result.Status,
				"exitCode": result.ExitCode,
				"ms":       result.RuntimeMs,
			})
		}

		return protocol.MustResponseOK(req.ID, result)
	}
}

func processKill(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if err := deps.Processes.Kill(p.ID); err != nil {
			return rpcerr.NotFound("process").Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]bool{"killed": true})
	}
}

func processGet(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		tracked := deps.Processes.Get(p.ID)
		if tracked == nil {
			return rpcerr.NotFound("process").Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, tracked)
	}
}

func processList(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, deps.Processes.List())
	}
}

// --- Cron methods ---

func cronList(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, deps.Cron.List())
	}
}

func cronGet(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		status := deps.Cron.Get(p.ID)
		if status == nil {
			return rpcerr.NotFound("cron task").Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, status)
	}
}

func cronUnregister(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		found := deps.Cron.Unregister(p.ID)
		return protocol.MustResponseOK(req.ID, map[string]bool{"removed": found})
	}
}

// --- Hook methods ---

func hooksList(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, deps.Hooks.List())
	}
}

func hooksRegister(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var hook hooks.Hook
		if err := json.Unmarshal(req.Params, &hook); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if err := deps.Hooks.Register(hook); err != nil {
			return rpcerr.Conflict(err.Error()).Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]bool{"registered": true})
	}
}

func hooksUnregister(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		found := deps.Hooks.Unregister(p.ID)
		return protocol.MustResponseOK(req.ID, map[string]bool{"removed": found})
	}
}

func hooksFire(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event hooks.Event       `json:"event"`
			Env   map[string]string `json:"env,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Event == "" {
			return rpcerr.MissingParam("event").Response(req.ID)
		}
		results := deps.Hooks.Fire(ctx, p.Event, p.Env)
		return protocol.MustResponseOK(req.ID, results)
	}
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

		return protocol.MustResponseOK(req.ID, result)
	}
}

func sessionsCreate(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key  string `json:"key"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Key == "" {
			return rpcerr.MissingParam("key").Response(req.ID)
		}
		// Validate session key format (Rust FFI or Go fallback).
		if err := ffi.ValidateSessionKey(p.Key); err != nil {
			return rpcerr.New(protocol.ErrValidationFailed, "invalid session key").
				WithSession(p.Key).Response(req.ID)
		}

		kind := session.Kind(protocol.ParseSessionKind(p.Kind))
		s := deps.Sessions.Create(p.Key, kind)
		if deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: p.Key,
				Reason:     "created",
			})
		}
		return protocol.MustResponseOK(req.ID, s)
	}
}

func sessionsLifecycle(deps ExtendedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key        string `json:"key"`
			Phase      string `json:"phase"`
			Ts         int64  `json:"ts"`
			StopReason string `json:"stopReason,omitempty"`
			Aborted    bool   `json:"aborted,omitempty"`
			StartedAt  *int64 `json:"startedAt,omitempty"`
			EndedAt    *int64 `json:"endedAt,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Key == "" || p.Phase == "" {
			return rpcerr.MissingParam("key and phase").Response(req.ID)
		}
		if err := ffi.ValidateSessionKey(p.Key); err != nil {
			return rpcerr.New(protocol.ErrValidationFailed, "invalid session key").
				WithSession(p.Key).Response(req.ID)
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

		// Fire session lifecycle hooks with panic recovery.
		if deps.Hooks != nil {
			var hookEvent hooks.Event
			switch session.LifecyclePhase(p.Phase) {
			case session.PhaseStart:
				hookEvent = hooks.EventSessionStart
			case session.PhaseEnd, session.PhaseError:
				hookEvent = hooks.EventSessionEnd
			}
			if hookEvent != "" {
				evt := hookEvent
				key := p.Key
				phase := p.Phase
				go func() {
					defer func() { recover() }()
					deps.Hooks.Fire(context.Background(), evt, map[string]string{
						"DENEB_SESSION_KEY": key,
						"DENEB_PHASE":       phase,
					})
				}()
			}
		}

		return protocol.MustResponseOK(req.ID, s)
	}
}

// --- Agents CRUD methods ---

func agentsList(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		agents := deps.Agents.List()
		if agents == nil {
			agents = make([]*agentpkg.Agent, 0)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"agents": agents})
	}
}

func agentsCreate(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID      string            `json:"agentId,omitempty"`
			Name         string            `json:"name,omitempty"`
			Description  string            `json:"description,omitempty"`
			Model        string            `json:"model,omitempty"`
			SystemPrompt string            `json:"systemPrompt,omitempty"`
			Tools        []string          `json:"tools,omitempty"`
			Metadata     map[string]string `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}

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

		return protocol.MustResponseOK(req.ID, map[string]any{"agent": created})
	}
}

func agentsUpdate(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string         `json:"agentId"`
			Patch   map[string]any `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.AgentID == "" {
			return rpcerr.MissingParam("agentId").Response(req.ID)
		}

		updated, err := deps.Agents.Update(p.AgentID, p.Patch)
		if err != nil {
			return rpcerr.NotFound("agent").WithAgent(p.AgentID).Response(req.ID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "updated",
				"agentId": p.AgentID,
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]any{"agent": updated})
	}
}

func agentsDelete(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.AgentID == "" {
			return rpcerr.MissingParam("agentId").Response(req.ID)
		}

		removed := deps.Agents.Delete(p.AgentID)
		if !removed {
			return rpcerr.NotFound("agent").WithAgent(p.AgentID).Response(req.ID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "deleted",
				"agentId": p.AgentID,
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]bool{"removed": true})
	}
}

func agentsFilesList(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.AgentID == "" {
			return rpcerr.MissingParam("agentId").Response(req.ID)
		}

		files, err := deps.Agents.ListFiles(p.AgentID)
		if err != nil {
			return rpcerr.NotFound("agent").WithAgent(p.AgentID).Response(req.ID)
		}
		if files == nil {
			files = make([]*agentpkg.FileEntry, 0)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{"files": files})
	}
}

func agentsFilesGet(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
			Name    string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.AgentID == "" || p.Name == "" {
			return rpcerr.MissingParam("agentId and name").Response(req.ID)
		}

		file, err := deps.Agents.GetFile(p.AgentID, p.Name)
		if err != nil {
			return rpcerr.NotFound("agent file").WithAgent(p.AgentID).Response(req.ID)
		}

		return protocol.MustResponseOK(req.ID, file)
	}
}

func agentsFilesSet(deps AgentsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID       string `json:"agentId"`
			Name          string `json:"name"`
			ContentBase64 string `json:"contentBase64,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.AgentID == "" || p.Name == "" {
			return rpcerr.MissingParam("agentId and name").Response(req.ID)
		}

		file, err := deps.Agents.SetFile(p.AgentID, p.Name, p.ContentBase64)
		if err != nil {
			return rpcerr.NotFound("agent").WithAgent(p.AgentID).Response(req.ID)
		}

		return protocol.MustResponseOK(req.ID, file)
	}
}

// --- Autonomous methods ---

func autonomousStatus(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		status := deps.Autonomous.Status()
		recentRuns := deps.Autonomous.RecentRuns(5)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"status":     status,
			"recentRuns": recentRuns,
		})
	}
}

func autonomousGoalsList(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		goals, err := deps.Autonomous.Goals().List()
		if err != nil {
			return rpcerr.Unavailable("failed to load goals: " + err.Error()).Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"goals": goals,
			"count": len(goals),
		})
	}
}

func autonomousGoalsAdd(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Description == "" {
			return rpcerr.MissingParam("description").Response(req.ID)
		}

		goal, err := deps.Autonomous.AddGoal(p.Description, p.Priority)
		if err != nil {
			return rpcerr.Unavailable("failed to add goal: " + err.Error()).Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, goal)
	}
}

func autonomousGoalsUpdate(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID       string `json:"id"`
			Priority string `json:"priority,omitempty"`
			Status   string `json:"status,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.Priority == "" && p.Status == "" {
			return rpcerr.MissingParam("priority or status").Response(req.ID)
		}

		if err := deps.Autonomous.Goals().UpdateGoal(p.ID, p.Priority, p.Status); err != nil {
			return rpcerr.NotFound("goal").Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"updated": p.ID})
	}
}

func autonomousGoalsRemove(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		if err := deps.Autonomous.Goals().Remove(p.ID); err != nil {
			return rpcerr.NotFound("goal").Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"removed": p.ID})
	}
}

func autonomousCycleRun(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Start cycle asynchronously -- don't block the RPC response.
		if err := deps.Autonomous.RunCycleAsync(); err != nil {
			return rpcerr.Unavailable(err.Error()).Response(req.ID)
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"started": true,
			"status":  "cycle started in background",
		})
	}
}

func autonomousCycleStop(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		deps.Autonomous.StopCycle()
		return protocol.MustResponseOK(req.ID, map[string]any{"stopped": true})
	}
}

func autonomousEnable(deps AutonomousDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		deps.Autonomous.SetEnabled(p.Enabled)
		return protocol.MustResponseOK(req.ID, map[string]any{"enabled": p.Enabled})
	}
}
