package process

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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
		p, errResp := rpcutil.DecodeParams[struct {
			Mode string `json:"mode"`
			Text string `json:"text"`
		}](req)
		if errResp != nil {
			return errResp
		}

		nextHeartbeat := deps.Cron.NextRunAt()

		if deps.Broadcaster != nil {
			deps.Broadcaster("wake", map[string]any{
				"mode": p.Mode,
				"text": p.Text,
				"ts":   time.Now().UnixMilli(),
			})
		}

		resp := rpcutil.RespondOK(req.ID, map[string]any{
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

		resp := rpcutil.RespondOK(req.ID, map[string]any{
			"running":     running,
			"nextRunAtMs": nextRun,
			"taskCount":   taskCount,
		})
		return resp
	}
}

func cronAdd(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Name       string `json:"name"`
			Schedule   string `json:"schedule"`
			Command    string `json:"command"`
			AgentID    string `json:"agentId,omitempty"`
			SessionKey string `json:"sessionKey,omitempty"`
			Enabled    *bool  `json:"enabled,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Name == "" || p.Schedule == "" || p.Command == "" {
			return rpcerr.New(protocol.ErrMissingParam, "name, schedule, and command are required").Response(req.ID)
		}
		const maxCommandLen = 4096
		if len(p.Command) > maxCommandLen {
			return rpcerr.New(protocol.ErrValidationFailed, "command exceeds maximum length of 4096 characters").Response(req.ID)
		}

		schedule, err := cron.ParseSchedule(p.Schedule)
		if err != nil {
			return rpcerr.Newf(protocol.ErrValidationFailed, "invalid schedule: %v", err).Response(req.ID)
		}

		// Use name as the task ID.
		schedule.Label = p.Name
		if regErr := deps.Cron.Register(ctx, p.Name, schedule, func(_ context.Context) error {
			// The actual cron command execution is handled by the task runner.
			return nil
		}); regErr != nil {
			return rpcerr.Wrap(protocol.ErrConflict, regErr).Response(req.ID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "added", "id": p.Name})
		}

		resp := rpcutil.RespondOK(req.ID, map[string]any{
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
		p, errResp := rpcutil.DecodeParams[struct {
			ID    string         `json:"id,omitempty"`
			JobID string         `json:"jobId,omitempty"`
			Patch map[string]any `json:"patch"`
		}](req)
		if errResp != nil {
			return errResp
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id or jobId is required").Response(req.ID)
		}

		if err := deps.Cron.Update(id, p.Patch); err != nil {
			return rpcerr.Wrap(protocol.ErrNotFound, err).Response(req.ID)
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("cron.changed", map[string]any{"action": "updated", "id": id})
		}

		status := deps.Cron.Get(id)
		resp := rpcutil.RespondOK(req.ID, status)
		return resp
	}
}

func cronRemove(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id or jobId is required").Response(req.ID)
		}

		removed := deps.Cron.Unregister(id)

		if deps.Broadcaster != nil && removed {
			deps.Broadcaster("cron.changed", map[string]any{"action": "removed", "id": id})
		}

		resp := rpcutil.RespondOK(req.ID, map[string]bool{"removed": removed})
		return resp
	}
}

func cronRun(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
			Mode  string `json:"mode,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id or jobId is required").Response(req.ID)
		}

		result, err := deps.Cron.RunNow(ctx, id)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrNotFound, err).Response(req.ID)
		}

		resp := rpcutil.RespondOK(req.ID, result)
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
			return rpcutil.RespondOK(req.ID, map[string]any{
				"runs":       page.Entries,
				"total":      page.Total,
				"hasMore":    page.HasMore,
				"nextOffset": page.NextOffset,
			})
		}

		runs := deps.Cron.Runs(id, limit, offset)
		resp := rpcutil.RespondOK(req.ID, map[string]any{
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
		"cron.getJob":   cronGetJob(deps),
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

		return rpcutil.RespondOK(req.ID, result)
	}
}

func cronGetJob(deps CronServiceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID    string `json:"id,omitempty"`
			JobID string `json:"jobId,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}

		id := p.ID
		if id == "" {
			id = p.JobID
		}
		if id == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id or jobId is required").Response(req.ID)
		}

		job := deps.Service.GetJob(id)
		if job == nil {
			return rpcerr.Newf(protocol.ErrNotFound, "job not found: %s", id).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, job)
	}
}
