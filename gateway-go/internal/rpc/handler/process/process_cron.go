package process

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)


// CronAdvancedDeps holds the dependencies for advanced cron RPC methods.
type CronAdvancedDeps struct {
	Cron        *cron.Scheduler
	RunLog      *cron.PersistentRunLog
	Broadcaster BroadcastFunc
}

// CronAdvancedMethods returns the advanced cron CRUD RPC handlers
// (wake, cron.status, cron.add, cron.update, cron.remove, cron.run, cron.runs).
// These complement the basic cron.list/get/unregister in the agent handler package.
func CronAdvancedMethods(deps CronAdvancedDeps) map[string]rpcutil.HandlerFunc {
	if deps.Cron == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"wake":        cronWake(deps),
		"cron.status": cronStatus(deps),
		"cron.add":    cronAdd(deps),
		"cron.update": cronUpdate(deps),
		"cron.remove": cronRemove(deps),
		"cron.run":    cronRun(deps),
		"cron.runs":   cronRuns(deps),
	}
}

func cronWake(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"nextHeartbeatAtMs": nextHeartbeat,
			"mode":              p.Mode,
		})
		return resp
	}
}

func cronStatus(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		running := deps.Cron.Running()
		nextRun := deps.Cron.NextRunAt()
		taskCount := len(deps.Cron.List())

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"running":     running,
			"nextRunAtMs": nextRun,
			"taskCount":   taskCount,
		})
		return resp
	}
}

func cronAdd(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"id":       p.Name,
			"name":     p.Name,
			"schedule": p.Schedule,
			"command":  p.Command,
		})
		return resp
	}
}

func cronUpdate(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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
		resp := protocol.MustResponseOK(req.ID, status)
		return resp
	}
}

func cronRemove(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]bool{"removed": removed})
		return resp
	}
}

func cronRun(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

func cronRuns(deps CronAdvancedDeps) rpcutil.HandlerFunc {
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

		// Prefer persistent run log when available; fall back to in-memory.
		if deps.RunLog != nil {
			opts := cron.RunLogReadOpts{
				Limit:   limit,
				Offset:  offset,
				Status:  p.Status,
				Query:   p.Query,
				SortDir: p.SortDir,
			}
			var page cron.RunLogPageResult
			if id != "" {
				page = deps.RunLog.ReadPage(id, opts)
			} else {
				page = deps.RunLog.ReadPageAll(opts)
			}
			return protocol.MustResponseOK(req.ID, map[string]any{
				"runs":       page.Entries,
				"total":      page.Total,
				"hasMore":    page.HasMore,
				"nextOffset": page.NextOffset,
			})
		}

		runs := deps.Cron.Runs(id, limit, offset)
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"runs":  runs,
			"total": len(runs),
		})
		return resp
	}
}

// CronServiceDeps holds the dependencies for cron.Service-backed RPC methods.
type CronServiceDeps struct {
	Service *cron.Service
}

// CronServiceMethods returns RPC handlers backed by cron.Service
// (cron.listPage, cron.get). These complement CronAdvancedMethods which
// use cron.Scheduler.
func CronServiceMethods(deps CronServiceDeps) map[string]rpcutil.HandlerFunc {
	if deps.Service == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"cron.listPage": cronListPage(deps),
		"cron.get":      cronGetJob(deps),
	}
}

func cronListPage(deps CronServiceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Limit           int    `json:"limit,omitempty"`
			Offset          int    `json:"offset,omitempty"`
			IncludeDisabled bool   `json:"includeDisabled,omitempty"`
			Query           string `json:"query,omitempty"`
			SortBy          string `json:"sortBy,omitempty"`
			SortDir         string `json:"sortDir,omitempty"`
		}
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}

		result := deps.Service.ListPage(cron.ListPageOptions{
			Limit:           p.Limit,
			Offset:          p.Offset,
			IncludeDisabled: p.IncludeDisabled,
			Query:           p.Query,
			SortBy:          p.SortBy,
			SortDir:         p.SortDir,
		})

		return protocol.MustResponseOK(req.ID, result)
	}
}

func cronGetJob(deps CronServiceDeps) rpcutil.HandlerFunc {
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

		job := deps.Service.GetJob(id)
		if job == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "job not found: "+id))
		}

		return protocol.MustResponseOK(req.ID, job)
	}
}
