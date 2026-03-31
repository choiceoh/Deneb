package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolAutoresearch creates the autoresearch ToolFunc.
// runner is the shared autoresearch runner managed by the server.
func ToolAutoresearch(runner *autoresearch.Runner) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action         string   `json:"action"`
			Workdir        string   `json:"workdir"`
			TargetFiles    []string `json:"target_files"`
			MetricCmd      string   `json:"metric_cmd"`
			MetricName     string   `json:"metric_name"`
			MetricDirection string  `json:"metric_direction"`
			TimeBudgetSec  int      `json:"time_budget_sec"`
			BranchTag      string   `json:"branch_tag"`
			Model          string   `json:"model"`
			Format         string   `json:"format"`
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
				p.MetricName, p.MetricDirection, p.TimeBudgetSec, p.BranchTag, p.Model)
		case "start":
			return autoresearchStart(runner, p.Workdir)
		case "stop":
			return autoresearchStop(runner)
		case "status":
			return autoresearchStatus(runner, p.Workdir)
		case "results":
			return autoresearchResults(p.Workdir, p.Format)
		default:
			return "", fmt.Errorf("unknown action: %s (use init, start, stop, status, or results)", p.Action)
		}
	}
}

func autoresearchInit(ctx context.Context, runner *autoresearch.Runner, workdir string,
	targetFiles []string, metricCmd, metricName, metricDirection string,
	timeBudgetSec int, branchTag, model string) (string, error) {

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

	// Save config to workspace (with baseline if available).
	if err := autoresearch.SaveConfig(workdir, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	return fmt.Sprintf("Autoresearch initialized in %s\n"+
		"Metric: %s (%s)\n"+
		"Target files: %v\n"+
		"Time budget: %ds/experiment\n"+
		"Branch tag: autoresearch/%s%s\n\n"+
		"Run autoresearch with action=start to begin the autonomous loop.",
		workdir, metricName, metricDirection, targetFiles, cfg.TimeBudgetSec, branchTag, baselineMsg), nil
}

func autoresearchStart(runner *autoresearch.Runner, workdir string) (string, error) {
	if err := runner.Start(workdir); err != nil {
		return "", err
	}
	cfg, _ := autoresearch.LoadConfig(workdir)
	if cfg != nil {
		return fmt.Sprintf("Autoresearch started: optimizing %s (%s)\n"+
			"Target: %v\n"+
			"Time budget: %ds/experiment\n"+
			"The loop runs autonomously. Use action=status to check progress, action=stop to halt.",
			cfg.MetricName, cfg.MetricDirection, cfg.TargetFiles, cfg.TimeBudgetSec), nil
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
		return "Autoresearch stopped.", nil
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

func autoresearchResults(workdir, format string) (string, error) {
	if format == "tsv" {
		return autoresearch.ReadResults(workdir)
	}
	// Default: summary.
	cfg, err := autoresearch.LoadConfig(workdir)
	if err != nil {
		return "", fmt.Errorf("no experiment found: %w", err)
	}
	results, _ := autoresearch.ReadResults(workdir)
	return autoresearch.Summary(workdir, cfg) + "\n" + results, nil
}
