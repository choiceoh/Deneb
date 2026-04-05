// Package autoresearch provides RPC handlers for direct autoresearch
// runner control, bypassing the chat pipeline.
package autoresearch

import (
	"context"

	ar "github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for autoresearch RPC methods.
type Deps struct {
	Runner *ar.Runner
}

// Methods returns the autoresearch.* RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"autoresearch.status":  arStatus(deps),
		"autoresearch.start":   arStart(deps),
		"autoresearch.stop":    arStop(deps),
		"autoresearch.results": arResults(deps),
		"autoresearch.config":  arConfig(deps),
		"autoresearch.resume":  arResume(deps),
		"autoresearch.archive": arArchive(deps),
		"autoresearch.runs":    arRuns(deps),
	}
}

func requireRunner(deps Deps, reqID string) *protocol.ResponseFrame {
	if deps.Runner == nil {
		return rpcerr.Unavailable("autoresearch runner not initialized").Response(reqID)
	}
	return nil
}

// --- autoresearch.status ---

func arStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}

		snap := deps.Runner.Status()
		result := map[string]any{
			"running":        snap.Running,
			"workdir":        snap.Workdir,
			"worktree_count": snap.WorktreeCount,
			"model":          snap.Model,
		}

		if snap.Workdir != "" {
			if cfg, err := ar.LoadConfig(snap.Workdir); err == nil {
				result["metric_name"] = cfg.MetricName
				result["metric_direction"] = cfg.MetricDirection
				result["target_files"] = cfg.TargetFiles
				result["branch_tag"] = cfg.BranchTag
				result["total_iterations"] = cfg.TotalIterations
				result["kept_iterations"] = cfg.KeptIterations
				result["consecutive_failures"] = cfg.ConsecutiveFailures
				result["baseline_metric"] = cfg.BaselineMetric
				result["best_metric"] = cfg.BestMetric
				result["max_iterations"] = cfg.Params.MaxIterations
			}
			if rows, err := ar.ParseResults(snap.Workdir); err == nil && len(rows) > 0 {
				result["last_result"] = rows[len(rows)-1]
			}
		}

		return rpcutil.RespondOK(req.ID, result)
	}
}

// --- autoresearch.start ---

func arStart(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir string `json:"workdir"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}
		if err := deps.Runner.Start(p.Workdir); err != nil {
			return rpcerr.Conflict(err.Error()).Response(req.ID)
		}
		snap := deps.Runner.Status()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":      true,
			"running": snap.Running,
			"workdir": snap.Workdir,
		})
	}
}

// --- autoresearch.stop ---

func arStop(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		if !deps.Runner.IsRunning() {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":      true,
				"running": false,
				"message": "autoresearch was not running",
			})
		}

		workdir := deps.Runner.Workdir()
		deps.Runner.Stop()

		result := map[string]any{
			"ok":      true,
			"running": false,
			"workdir": workdir,
		}
		if cfg, err := ar.LoadConfig(workdir); err == nil {
			result["total_iterations"] = cfg.TotalIterations
			result["kept_iterations"] = cfg.KeptIterations
			result["best_metric"] = cfg.BestMetric
			result["baseline_metric"] = cfg.BaselineMetric
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

// --- autoresearch.results ---

func arResults(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir string `json:"workdir"`
			Format  string `json:"format"`
		}](req)
		if errResp != nil {
			return errResp
		}

		workdir := p.Workdir
		if workdir == "" {
			workdir = deps.Runner.Workdir()
		}
		if workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}

		if p.Format == "tsv" {
			raw, err := ar.ReadResults(workdir)
			if err != nil {
				return rpcerr.NotFound("results").Response(req.ID)
			}
			return rpcutil.RespondOK(req.ID, map[string]any{"tsv": raw})
		}

		// Default: structured JSON.
		rows, err := ar.ParseResults(workdir)
		if err != nil {
			return rpcerr.NotFound("results").Response(req.ID)
		}

		result := map[string]any{
			"rows":  rows,
			"count": len(rows),
		}
		if cfg, loadErr := ar.LoadConfig(workdir); loadErr == nil && len(rows) > 0 {
			result["trend"] = ar.TrendAnalysis(rows, cfg)
			result["summary"] = ar.Summary(workdir, cfg)
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

// --- autoresearch.config ---

func arConfig(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir         string           `json:"workdir"`
			TargetFiles     []string         `json:"target_files,omitempty"`
			MetricCmd       string           `json:"metric_cmd,omitempty"`
			MetricName      string           `json:"metric_name,omitempty"`
			MetricDirection string           `json:"metric_direction,omitempty"`
			TimeBudgetSec   int              `json:"time_budget_sec,omitempty"`
			BranchTag       string           `json:"branch_tag,omitempty"`
			Model           string           `json:"model,omitempty"`
			MetricPattern   string           `json:"metric_pattern,omitempty"`
			MaxIterations   int              `json:"max_iterations,omitempty"`
			CacheEnabled    bool             `json:"cache_enabled,omitempty"`
			Constants       []ar.ConstantDef `json:"constants,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}

		// Load existing or create new.
		cfg, _ := ar.LoadConfig(p.Workdir)
		if cfg == nil {
			cfg = &ar.Config{}
		}

		// Apply provided fields.
		if len(p.TargetFiles) > 0 {
			cfg.TargetFiles = p.TargetFiles
		}
		if p.MetricCmd != "" {
			cfg.MetricCmd = p.MetricCmd
		}
		if p.MetricName != "" {
			cfg.MetricName = p.MetricName
		}
		if p.MetricDirection != "" {
			cfg.MetricDirection = p.MetricDirection
		}
		if p.TimeBudgetSec > 0 {
			cfg.TimeBudgetSec = p.TimeBudgetSec
		}
		if p.BranchTag != "" {
			cfg.BranchTag = p.BranchTag
		}
		if p.Model != "" {
			cfg.Model = p.Model
		}
		if p.MetricPattern != "" {
			cfg.MetricPattern = p.MetricPattern
		}
		if p.MaxIterations != 0 {
			cfg.Params.MaxIterations = p.MaxIterations
		}
		if p.CacheEnabled {
			cfg.CacheEnabled = true
		}
		if len(p.Constants) > 0 {
			cfg.Constants = p.Constants
		}

		if err := cfg.Validate(); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		if err := ar.SaveConfig(p.Workdir, cfg); err != nil {
			return rpcerr.New(protocol.ErrUnavailable, err.Error()).Response(req.ID)
		}

		deps.Runner.SetWorkdir(p.Workdir)

		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":     true,
			"config": cfg,
		})
	}
}

// --- autoresearch.resume ---

func arResume(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir string `json:"workdir"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}

		cfg, err := ar.LoadConfig(p.Workdir)
		if err != nil {
			return rpcerr.NotFound("config").Response(req.ID)
		}
		if cfg.TotalIterations == 0 {
			return rpcerr.InvalidRequest("no iterations completed — use start instead").Response(req.ID)
		}

		cfg.Resume = true
		if err := ar.SaveConfig(p.Workdir, cfg); err != nil {
			return rpcerr.New(protocol.ErrUnavailable, err.Error()).Response(req.ID)
		}

		if err := deps.Runner.Start(p.Workdir); err != nil {
			return rpcerr.Conflict(err.Error()).Response(req.ID)
		}
		snap := deps.Runner.Status()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":               true,
			"running":          snap.Running,
			"resumed_from":     cfg.TotalIterations,
			"best_metric":      cfg.BestMetric,
			"baseline_metric":  cfg.BaselineMetric,
		})
	}
}

// --- autoresearch.archive ---

func arArchive(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir string `json:"workdir"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}

		cfg, err := ar.LoadConfig(p.Workdir)
		if err != nil {
			return rpcerr.NotFound("config").Response(req.ID)
		}
		path, err := ar.ArchiveRun(p.Workdir, cfg.BranchTag)
		if err != nil {
			return rpcerr.New(protocol.ErrUnavailable, err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":   true,
			"path": path,
			"tag":  cfg.BranchTag,
		})
	}
}

// --- autoresearch.runs ---

func arRuns(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireRunner(deps, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[struct {
			Workdir string `json:"workdir"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Workdir == "" {
			return rpcerr.MissingParam("workdir").Response(req.ID)
		}

		runs, err := ar.ListRuns(p.Workdir)
		if err != nil {
			return rpcerr.New(protocol.ErrUnavailable, err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":    true,
			"runs":  runs,
			"count": len(runs),
		})
	}
}
