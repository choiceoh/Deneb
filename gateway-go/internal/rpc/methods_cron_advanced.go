package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CronAdvancedDeps holds the dependencies for advanced cron RPC methods.
type CronAdvancedDeps struct {
	Cron        *cron.Scheduler
	Broadcaster BroadcastFunc
}

// RegisterCronAdvancedMethods registers the advanced cron CRUD RPC methods
// (wake, cron.status, cron.add, cron.update, cron.remove, cron.run, cron.runs).
// These complement the basic cron.list/get/unregister in methods_agent.go.
func RegisterCronAdvancedMethods(d *Dispatcher, deps CronAdvancedDeps) {
	if deps.Cron == nil {
		return
	}

	d.Register("wake", cronWake(deps))
	d.Register("cron.status", cronStatus(deps))
	d.Register("cron.add", cronAdd(deps))
	d.Register("cron.update", cronUpdate(deps))
	d.Register("cron.remove", cronRemove(deps))
	d.Register("cron.run", cronRun(deps))
	d.Register("cron.runs", cronRuns(deps))
}

func cronWake(deps CronAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Mode string `json:"mode"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		nextHeartbeat := deps.Cron.NextRunAt()

		if deps.Broadcaster != nil {
			deps.Broadcaster("wake", map[string]any{
				"mode": p.Mode,
				"text": p.Text,
				"ts":   time.Now().UnixMilli(),
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"nextHeartbeatAtMs": nextHeartbeat,
			"mode":              p.Mode,
		})
		return resp
	}
}

func cronStatus(deps CronAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		running := deps.Cron.Running()
		nextRun := deps.Cron.NextRunAt()
		taskCount := len(deps.Cron.List())

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"running":       running,
			"nextRunAtMs":   nextRun,
			"taskCount":     taskCount,
		})
		return resp
	}
}

func cronAdd(deps CronAdvancedDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Name       string `json:"name"`
			Schedule   string `json:"schedule"`
			Command    string `json:"command"`
			AgentID    string `json:"agentId,omitempty"`
			SessionKey string `json:"sessionKey,omitempty"`
			Enabled    *bool  `json:"enabled,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Name == "" || p.Schedule == "" || p.Command == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "name, schedule, and command are required"))
		}
		const maxCommandLen = 4096
		if len(p.Command) > maxCommandLen {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "command exceeds maximum length of 4096 characters"))
		}

		schedule, err := cron.ParseSchedule(p.Schedule)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid schedule: "+err.Error()))
		}

		// Use name as the task ID.
		schedule.Label = p.Name
		if regErr := deps.Cron.Register(ctx, p.Name, schedule, func(_ context.Context) error {
			// The actual cron command execution is handled by the task runner.
			return nil
		}); regErr != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, regErr.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "added", "id": p.Name})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"id":       p.Name,
			"name":     p.Name,
			"schedule": p.Schedule,
			"command":  p.Command,
		})
		return resp
	}
}

func cronUpdate(deps CronAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string         `json:"id,omitempty"`
			JobID string         `json:"jobId,omitempty"`
			Patch map[string]any `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		if err := deps.Cron.Update(id, p.Patch); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "updated", "id": id})
		}

		status := deps.Cron.Get(id)
		resp, _ := protocol.NewResponseOK(req.ID, status)
		return resp
	}
}

func cronRemove(deps CronAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		removed := deps.Cron.Unregister(id)

		if deps.Broadcaster != nil && removed {
			deps.Broadcaster("cron.changed", map[string]any{"action": "removed", "id": id})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"removed": removed})
		return resp
	}
}

func cronRun(deps CronAdvancedDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
			Mode  string `json:"mode,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id or jobId is required"))
		}

		result, err := deps.Cron.RunNow(ctx, id)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

func cronRuns(deps CronAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Scope            string   `json:"scope,omitempty"`
			ID               string   `json:"id,omitempty"`
			JobID            string   `json:"jobId,omitempty"`
			Limit            int      `json:"limit,omitempty"`
			Offset           int      `json:"offset,omitempty"`
			Statuses         []string `json:"statuses,omitempty"`
			Status           string   `json:"status,omitempty"`
			DeliveryStatuses []string `json:"deliveryStatuses,omitempty"`
			Query            string   `json:"query,omitempty"`
			SortDir          string   `json:"sortDir,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		id := p.ID
		if id == "" {
			id = p.JobID
		}

		// Cap pagination to prevent pathologically large queries.
		limit := p.Limit
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
		offset := p.Offset
		if offset < 0 {
			offset = 0
		}

		runs := deps.Cron.Runs(id, limit, offset)

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"runs":  runs,
			"total": len(runs),
		})
		return resp
	}
}
