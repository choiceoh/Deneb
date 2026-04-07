package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolAutoresearch creates the autoresearch ToolFunc.
// runner is the shared autoresearch runner managed by the server.
func ToolAutoresearch(runner *autoresearch.Runner) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action          string                     `json:"action"`
			Workdir         string                     `json:"workdir"`
			TargetFiles     []string                   `json:"target_files"`
			MetricCmd       string                     `json:"metric_cmd"`
			MetricName      string                     `json:"metric_name"`
			MetricDirection string                     `json:"metric_direction"`
			TimeBudgetSec   int                        `json:"time_budget_sec"`
			BranchTag       string                     `json:"branch_tag"`
			Model           string                     `json:"model"`
			MetricPattern   string                     `json:"metric_pattern"`
			MaxIterations   int                        `json:"max_iterations"`
			CacheEnabled    bool                       `json:"cache_enabled"`
			Format          string                     `json:"format"`
			Constants       []autoresearch.ConstantDef `json:"constants"`
			AutoStart       bool                       `json:"auto_start"`
		}
		if err := jsonutil.UnmarshalInto("autoresearch params", input, &p); err != nil {
			return "", err
		}
		if p.Workdir == "" {
			return "", fmt.Errorf("workdir is required")
		}

		switch p.Action {
		case "init":
			return autoresearchInit(ctx, runner, p.Workdir, p.TargetFiles, p.MetricCmd,
				p.MetricName, p.MetricDirection, p.TimeBudgetSec, p.BranchTag, p.Model, p.MetricPattern, p.MaxIterations, p.CacheEnabled, p.Constants)
		case "start":
			return autoresearchStart(ctx, runner, p.Workdir)
		case "stop":
			return autoresearchStop(runner)
		case "status":
			return autoresearchStatus(runner, p.Workdir)
		case "results":
			return autoresearchResults(p.Workdir, p.Format)
		case "update_constants":
			return autoresearchUpdateConstants(runner, p.Workdir, p.Constants, p.AutoStart)
		case "resume":
			return autoresearchResume(ctx, runner, p.Workdir)
		case "archive":
			return autoresearchArchive(p.Workdir)
		case "runs":
			return autoresearchListRuns(p.Workdir)
		case "apply_overrides":
			return autoresearchApplyOverrides(p.Workdir)
		default:
			return "", fmt.Errorf("unknown action: %s (use init, start, stop, status, results, resume, archive, runs, update_constants, or apply_overrides)", p.Action)
		}
	}
}

func autoresearchInit(ctx context.Context, runner *autoresearch.Runner, workdir string,
	targetFiles []string, metricCmd, metricName, metricDirection string,
	timeBudgetSec int, branchTag, model, metricPattern string, maxIterations int,
	cacheEnabled bool, constants []autoresearch.ConstantDef) (string, error) {

	if runner.IsRunning() {
		return "", fmt.Errorf("autoresearch already running — stop it first")
	}

	cfg := &autoresearch.Config{
		TargetFiles:     targetFiles,
		MetricCmd:       metricCmd,
		MetricName:      metricName,
		MetricDirection: metricDirection,
		TimeBudgetSec:   timeBudgetSec,
		BranchTag:       branchTag,
		Model:           model,
		MetricPattern:   metricPattern,
		CacheEnabled:    cacheEnabled,
		Constants:       constants,
		Params:          autoresearch.Params{MaxIterations: maxIterations},
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	// Run baseline measurement so we have a reference point.
	baselineMsg := ""
	baselineMetric, baseErr := autoresearch.RunBaseline(ctx, workdir, cfg)
	if baseErr != nil {
		baselineMsg = fmt.Sprintf("\nBaseline run failed: %v\nYou can still start the loop — first successful iteration becomes the baseline.", baseErr)
	} else {
		cfg.BaselineMetric = &baselineMetric
		cfg.BestMetric = &baselineMetric
		baselineMsg = fmt.Sprintf("\nBaseline %s: %.6f", metricName, baselineMetric)
	}

	// Pre-flight: verify constant patterns actually match the source files.
	// This catches tab/space indentation mismatches before the experiment starts,
	// preventing the agent from entering a discover→retry loop.
	if cfg.IsConstantsMode() {
		if _, extractErr := autoresearch.ExtractConstants(workdir, cfg.Constants); extractErr != nil {
			return "", fmt.Errorf("constants pattern pre-flight failed: %w -- "+
				"fix the pattern and re-run init; the pattern must match the actual file content "+
				"(check for tab indentation in Go const blocks)", extractErr)
		}
	}

	// Save config to workspace (with baseline if available).
	if err := autoresearch.SaveConfig(workdir, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	// Record workdir so /chart can find the experiment.
	runner.SetWorkdir(workdir)

	modeStr := "file-rewrite"
	if cfg.IsConstantsMode() {
		modeStr = fmt.Sprintf("constants-override (%d constants)", len(cfg.Constants))
	}

	iterInfo := fmt.Sprintf("%d iterations", cfg.Params.MaxIterations)
	if cfg.Params.MaxIterations == 0 {
		iterInfo = "unlimited"
	}

	cacheInfo := "disabled"
	if cfg.CacheEnabled {
		cacheInfo = cfg.ResolveCacheDir(workdir)
	}

	return fmt.Sprintf("Autoresearch initialized in %s\n"+
		"Mode: %s\n"+
		"Metric: %s (%s)\n"+
		"Target files: %v\n"+
		"Time budget: %ds/experiment\n"+
		"Max iterations: %s (auto-stop + report)\n"+
		"Cache: %s\n"+
		"Branch tag: autoresearch/%s%s\n\n"+
		"Run autoresearch with action=start to begin the autonomous loop.",
		workdir, modeStr, metricName, metricDirection, targetFiles, cfg.TimeBudgetSec, iterInfo, cacheInfo, branchTag, baselineMsg), nil
}

func autoresearchStart(ctx context.Context, runner *autoresearch.Runner, workdir string) (string, error) {
	// Record the triggering session so completion results are injected
	// into its transcript for the LLM's next turn.
	if key := toolctx.SessionKeyFromContext(ctx); key != "" {
		runner.SetSessionKey(key)
	}
	if err := runner.Start(workdir); err != nil {
		return "", err
	}
	cfg, _ := autoresearch.LoadConfig(workdir)
	if cfg != nil {
		iterInfo := fmt.Sprintf("auto-stop after %d", cfg.Params.MaxIterations)
		if cfg.Params.MaxIterations == 0 {
			iterInfo = "unlimited (manual stop)"
		}
		return fmt.Sprintf("Autoresearch started: optimizing %s (%s)\n"+
			"Branch: autoresearch/%s\n"+
			"Target: %v\n"+
			"Time budget: %ds/experiment\n"+
			"Iterations: %s\n"+
			"The loop runs autonomously. A completion report with chart will be sent when done.",
			cfg.MetricName, cfg.MetricDirection, cfg.BranchTag, cfg.TargetFiles, cfg.TimeBudgetSec, iterInfo), nil
	}
	return "Autoresearch started.", nil
}

func autoresearchStop(runner *autoresearch.Runner) (string, error) {
	if !runner.IsRunning() {
		return "Autoresearch is not running.", nil
	}
	workdir := runner.Workdir()
	runner.Stop()

	// Return final summary.
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "Autoresearch stopped.", nil //nolint:nilerr // gracefully degrade when config is unavailable
	}
	return "Autoresearch stopped.\n\n" + autoresearch.Summary(workdir, cfg), nil
}

func autoresearchStatus(runner *autoresearch.Runner, workdir string) (string, error) {
	running := runner.IsRunning()
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		if running {
			return "Autoresearch is running but config not found.", nil
		}
		return "No autoresearch experiment found in " + workdir, nil
	}

	status := "RUNNING"
	if !running {
		status = "STOPPED"
	}
	return fmt.Sprintf("[%s]\n\n%s", status, autoresearch.Summary(workdir, cfg)), nil
}

func autoresearchUpdateConstants(runner *autoresearch.Runner, workdir string, constants []autoresearch.ConstantDef, autoStart bool) (string, error) {
	if runner.IsRunning() {
		runner.Stop()
	}

	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	if len(constants) == 0 {
		return "", fmt.Errorf("constants is required for update_constants")
	}

	// Update constants in the existing config.
	cfg.Constants = constants

	// Validate the updated config.
	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid constants: %w", err)
	}

	// Pre-flight: verify patterns match actual file content.
	extracted, extractErr := autoresearch.ExtractConstants(workdir, cfg.Constants)
	if extractErr != nil {
		return "", fmt.Errorf("pattern pre-flight failed: %w -- fix the pattern and retry", extractErr)
	}

	// Save updated config.
	if err := autoresearch.SaveConfig(workdir, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("Constants updated and verified:\n")
	for _, cd := range constants {
		fmt.Fprintf(&sb, "  %s = %s (pattern OK)\n", cd.Name, extracted[cd.Name])
	}

	if autoStart {
		if startErr := runner.Start(workdir); startErr != nil {
			fmt.Fprintf(&sb, "\nAuto-start failed: %v", startErr)
		} else {
			sb.WriteString("\nExperiment restarted automatically.")
		}
	} else {
		sb.WriteString("\nRun action=start to resume the experiment.")
	}

	return sb.String(), nil
}

func autoresearchApplyOverrides(workdir string) (string, error) {
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsConstantsMode() {
		return "", fmt.Errorf("constants mode not configured — no overrides to apply")
	}
	ov, err := autoresearch.LoadOverrides(workdir)
	if err != nil {
		return "", fmt.Errorf("load overrides: %w", err)
	}
	if len(ov.Values) == 0 {
		return "No overrides found in overrides.json.", nil
	}

	// Apply permanently — intentionally do not call restore.
	_, err = autoresearch.ApplyOverrides(workdir, cfg.Constants, ov.Values)
	if err != nil {
		return "", fmt.Errorf("apply overrides: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("Applied overrides to source files:\n")
	for name, val := range ov.Values {
		fmt.Fprintf(&sb, "  %s = %s\n", name, val)
	}
	sb.WriteString("\nThe overrides are now baked into the source files. Commit when ready.")
	return sb.String(), nil
}

func autoresearchResume(ctx context.Context, runner *autoresearch.Runner, workdir string) (string, error) {
	if runner.IsRunning() {
		return "", fmt.Errorf("autoresearch already running — stop it first")
	}
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "", fmt.Errorf("no experiment found to resume: %w", err)
	}
	if cfg.TotalIterations == 0 {
		return "", fmt.Errorf("no iterations completed — use action=start instead")
	}

	cfg.Resume = true
	if err := autoresearch.SaveConfig(workdir, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	if key := toolctx.SessionKeyFromContext(ctx); key != "" {
		runner.SetSessionKey(key)
	}
	if err := runner.Start(workdir); err != nil {
		return "", err
	}

	return fmt.Sprintf("Autoresearch resumed from iteration %d\n"+
		"Metric: %s (%s)\n"+
		"Branch: autoresearch/%s\n"+
		"Best so far: %v\n"+
		"The loop continues autonomously.",
		cfg.TotalIterations, cfg.MetricName, cfg.MetricDirection,
		cfg.BranchTag, cfg.BestMetric), nil
}

func autoresearchArchive(workdir string) (string, error) {
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "", fmt.Errorf("no experiment found: %w", err)
	}
	path, err := autoresearch.ArchiveRun(workdir, cfg.BranchTag)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Experiment archived to %s\n"+
		"Metric: %s, Iterations: %d, Best: %v",
		path, cfg.MetricName, cfg.TotalIterations, cfg.BestMetric), nil
}

func autoresearchListRuns(workdir string) (string, error) {
	runs, err := autoresearch.ListRuns(workdir)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "No archived runs found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Archived runs (%d):\n\n", len(runs))
	for _, run := range runs {
		fmt.Fprintf(&sb, "  %s — %s (%s)\n", run.Tag, run.MetricName, run.Direction)
		fmt.Fprintf(&sb, "    Iterations: %d, Kept: %d\n", run.TotalIterations, run.KeptIterations)
		if run.BaselineMetric != nil && run.BestMetric != nil {
			fmt.Fprintf(&sb, "    Baseline: %.6f, Best: %.6f\n", *run.BaselineMetric, *run.BestMetric)
		}
		fmt.Fprintf(&sb, "    Archived: %s\n\n", run.ArchivedAt)
	}
	return sb.String(), nil
}

func autoresearchResults(workdir, format string) (string, error) {
	switch format {
	case "tsv":
		return autoresearch.ReadResults(workdir)

	case "chart":
		cfg, err := autoresearch.LoadConfig(workdir)
		if err != nil {
			return "", fmt.Errorf("no experiment found: %w", err)
		}
		rows, err := autoresearch.ParseResults(workdir)
		if err != nil {
			return "", fmt.Errorf("parse results: %w", err)
		}
		path, err := autoresearch.SaveChart(workdir, rows, cfg)
		if err != nil {
			return "", fmt.Errorf("generate chart: %w", err)
		}
		return fmt.Sprintf("Chart saved to %s\nSend this file to view the experiment progress graph.", path), nil

	default:
		// Default: summary.
		cfg, err := autoresearch.LoadConfig(workdir)
		if err != nil {
			return "", fmt.Errorf("no experiment found: %w", err)
		}
		results, _ := autoresearch.ReadResults(workdir)
		return autoresearch.Summary(workdir, cfg) + "\n" + results, nil
	}
}
