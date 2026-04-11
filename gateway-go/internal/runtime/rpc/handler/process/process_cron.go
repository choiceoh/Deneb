package process

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// resolveJobID returns the first non-empty of id or jobID, or an error.
func resolveJobID(id, jobID string) (string, error) {
	if id != "" {
		return id, nil
	}
	if jobID != "" {
		return jobID, nil
	}
	return "", rpcerr.New(protocol.ErrMissingParam, "id or jobId is required")
}

// emitCronChanged broadcasts a cron.changed event if a broadcaster is set.
func emitCronChanged(b BroadcastFunc, action, id string) {
	if b != nil {
		b("cron.changed", map[string]any{"action": action, "id": id})
	}
}

// CronAdvancedDeps holds the dependencies for advanced cron RPC methods.
type CronAdvancedDeps struct {
	Service     *cron.Service
	RunLog      *cron.PersistentRunLog
	Broadcaster BroadcastFunc
}

// CronAdvancedMethods returns the advanced cron CRUD RPC handlers
// (cron.status, cron.add, cron.update, cron.remove, cron.run, cron.runs).
func CronAdvancedMethods(deps CronAdvancedDeps) map[string]rpcutil.HandlerFunc {
	if deps.Service == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"cron.status": cronStatus(deps),
		"cron.add":    cronAdd(deps),
		"cron.update": cronUpdate(deps),
		"cron.remove": cronRemove(deps),
		"cron.run":    cronRun(deps),
		"cron.runs":   cronRuns(deps),
	}
}

func cronStatus(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		status := deps.Service.Status()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"running":     status.Running,
			"nextRunAtMs": status.NextRunAtMs,
			"taskCount":   status.TaskCount,
		})
	}
}

func cronAdd(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID       string `json:"id,omitempty"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		AgentID  string `json:"agentId,omitempty"`
		Enabled  *bool  `json:"enabled,omitempty"`

		// Extended fields for persistent cron jobs.
		Delivery     *cron.JobDeliveryConfig `json:"delivery,omitempty"`
		FailureAlert *cron.CronFailureAlert  `json:"failureAlert,omitempty"`
		Tz           string                  `json:"tz,omitempty"`
		StaggerMs    int64                   `json:"staggerMs,omitempty"`
		AnchorTime   string                  `json:"anchorTime,omitempty"`
		RetryCount   int                     `json:"retryCount,omitempty"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.Name == "" || p.Schedule == "" || p.Command == "" {
			return nil, rpcerr.New(protocol.ErrMissingParam, "name, schedule, and command are required")
		}
		const maxCommandLen = 4096
		if len(p.Command) > maxCommandLen {
			return nil, rpcerr.New(protocol.ErrValidationFailed, "command exceeds maximum length of 4096 characters")
		}

		schedule, err := cron.ParseSmartScheduleWithOpts(p.Schedule, cron.SmartScheduleOpts{
			Tz:         p.Tz,
			StaggerMs:  p.StaggerMs,
			AnchorTime: p.AnchorTime,
		})
		if err != nil {
			return nil, rpcerr.Newf(protocol.ErrValidationFailed, "invalid schedule: %v", err)
		}

		id := p.ID
		if id == "" {
			id = p.Name
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}

		job := cron.StoreJob{
			ID:       id,
			Name:     p.Name,
			AgentID:  p.AgentID,
			Enabled:  enabled,
			Schedule: schedule,
			Payload: cron.StorePayload{
				Kind:       "agentTurn",
				Message:    p.Command,
				RetryCount: p.RetryCount,
			},
			Delivery:     p.Delivery,
			FailureAlert: p.FailureAlert,
		}

		if err := deps.Service.Add(ctx, job); err != nil {
			return nil, rpcerr.Wrap(protocol.ErrConflict, err)
		}

		emitCronChanged(deps.Broadcaster, "added", id)

		return map[string]any{
			"id":       id,
			"name":     p.Name,
			"schedule": p.Schedule,
			"command":  p.Command,
			"enabled":  enabled,
		}, nil
	})
}

func cronUpdate(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID    string         `json:"id,omitempty"`
		JobID string         `json:"jobId,omitempty"`
		Patch map[string]any `json:"patch"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		id, err := resolveJobID(p.ID, p.JobID)
		if err != nil {
			return nil, err
		}

		err = deps.Service.Update(ctx, id, func(job *cron.StoreJob) {
			if name, ok := p.Patch["name"]; ok {
				if s, ok := name.(string); ok {
					job.Name = s
				}
			}
			if enabled, ok := p.Patch["enabled"]; ok {
				if b, ok := enabled.(bool); ok {
					job.Enabled = b
				}
			}
			if command, ok := p.Patch["command"]; ok {
				if s, ok := command.(string); ok {
					job.Payload.Message = s
				}
			}
			if schedule, ok := p.Patch["schedule"]; ok {
				if s, ok := schedule.(string); ok {
					if parsed, err := cron.ParseSmartSchedule(s); err == nil {
						job.Schedule = parsed
					}
				}
			}
			if agentID, ok := p.Patch["agentId"]; ok {
				if s, ok := agentID.(string); ok {
					job.AgentID = s
				}
			}
		})
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrNotFound, err)
		}

		emitCronChanged(deps.Broadcaster, "updated", id)

		job := deps.Service.Job(id)
		return job, nil
	})
}

func cronRemove(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID    string `json:"id,omitempty"`
		JobID string `json:"jobId,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		id, err := resolveJobID(p.ID, p.JobID)
		if err != nil {
			return nil, err
		}

		removed := deps.Service.Remove(id) == nil
		if removed {
			emitCronChanged(deps.Broadcaster, "removed", id)
		}
		return map[string]bool{"removed": removed}, nil
	})
}

func cronRun(deps CronAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID    string `json:"id,omitempty"`
		JobID string `json:"jobId,omitempty"`
		Mode  string `json:"mode,omitempty"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		id, err := resolveJobID(p.ID, p.JobID)
		if err != nil {
			return nil, err
		}

		outcome, err := deps.Service.Run(ctx, id, p.Mode)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrNotFound, err)
		}
		return map[string]any{
			"id":         id,
			"status":     outcome.Status,
			"error":      outcome.Error,
			"durationMs": outcome.DurationMs,
		}, nil
	})
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

		return rpcutil.RespondOK(req.ID, map[string]any{
			"runs":  []any{},
			"total": 0,
		})
	}
}

// CronServiceDeps holds the dependencies for cron.Service-backed RPC methods.
type CronServiceDeps struct {
	Service *cron.Service
}

// CronServiceMethods returns RPC handlers backed by cron.Service
// (cron.listPage, cron.getJob).
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
	type params struct {
		ID    string `json:"id,omitempty"`
		JobID string `json:"jobId,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		id, err := resolveJobID(p.ID, p.JobID)
		if err != nil {
			return nil, err
		}

		job := deps.Service.Job(id)
		if job == nil {
			return nil, rpcerr.Newf(protocol.ErrNotFound, "job not found: %s", id)
		}

		return job, nil
	})
}
