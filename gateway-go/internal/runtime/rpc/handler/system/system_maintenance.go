// system_maintenance.go — maintenance.* and update.* RPC handlers.
package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MaintenanceDeps holds dependencies for maintenance RPC methods.
type MaintenanceDeps struct {
	Runner *maintenance.Runner
}

// MaintenanceMethods returns the maintenance.run, maintenance.status,
// and maintenance.summary handlers.
func MaintenanceMethods(deps MaintenanceDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"maintenance.run":     maintenanceRun(deps),
		"maintenance.status":  maintenanceStatus(deps),
		"maintenance.summary": maintenanceSummary(deps),
	}
}

func maintenanceRun(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return rpcerr.Unavailable("maintenance runner not available").Response(req.ID)
		}

		var p struct {
			DryRun bool `json:"dryRun"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		report := deps.Runner.Run(p.DryRun)
		resp, _ := protocol.NewResponseOK(req.ID, report)
		return resp
	}
}

func maintenanceStatus(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		report := deps.Runner.LastReport()
		if report == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"hasReport": true,
			"report":    report,
			"summary":   maintenance.SummarizeReport(report),
		})
		return resp
	}
}

func maintenanceSummary(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return rpcerr.Unavailable("maintenance runner not available").Response(req.ID)
		}

		report := deps.Runner.LastReport()
		if report == nil {
			// No previous report -- trigger a dry-run.
			report = deps.Runner.Run(true)
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"summary": maintenance.SummarizeReport(report),
			"report":  report,
		})
		return resp
	}
}

// UpdateDeps holds dependencies for the update.run RPC method.
type UpdateDeps struct {
	// RepoDir is the repository root directory for git operations.
	// Defaults to the current working directory if empty.
	RepoDir string
	// DenebDir is the deneb config directory (~/.deneb) for sentinel file.
	DenebDir string
}

// UpdateMethods returns the update.run handler.
func UpdateMethods(deps UpdateDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"update.run": updateRun(deps),
	}
}

// updateStep records a single step in the update process.
type updateStep struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Log        string `json:"log,omitempty"`
	OK         bool   `json:"ok"`
}

func updateRun(deps UpdateDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			TimeoutMs      int64  `json:"timeoutMs"`
			RestartDelayMs int64  `json:"restartDelayMs"`
			Note           string `json:"note"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 120_000
		}
		timeout := time.Duration(p.TimeoutMs) * time.Millisecond
		updateCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		repoDir := deps.RepoDir
		if repoDir == "" {
			var err error
			repoDir, err = os.Getwd()
			if err != nil {
				return rpcerr.WrapUnavailable("cannot determine working directory", err).Response(req.ID)
			}
		}

		startTime := time.Now()
		var steps []updateStep

		// Step 1: Get current git SHA (before).
		beforeSHA := runGitRev(repoDir)

		// Step 2: git pull --rebase origin main.
		pullStep := runStep(updateCtx, repoDir, "git pull", "git", "pull", "--rebase", "origin", "main")
		steps = append(steps, pullStep)
		if !pullStep.OK {
			return updateResult(updateResultOpts{
				reqID: req.ID, ok: false, status: "error", mode: "git",
				beforeSHA: beforeSHA, afterSHA: beforeSHA,
				steps: steps, startTime: startTime,
				denebDir: deps.DenebDir, restartDelayMs: p.RestartDelayMs,
			})
		}

		// Step 3: Get new git SHA (after).
		afterSHA := runGitRev(repoDir)

		// Step 4: Rebuild via make.
		buildStep := runStep(updateCtx, repoDir, "make all", "make", "all")
		steps = append(steps, buildStep)
		if !buildStep.OK {
			return updateResult(updateResultOpts{
				reqID: req.ID, ok: false, status: "error", mode: "make",
				beforeSHA: beforeSHA, afterSHA: afterSHA,
				steps: steps, startTime: startTime,
				denebDir: deps.DenebDir, restartDelayMs: p.RestartDelayMs,
			})
		}

		return updateResult(updateResultOpts{
			reqID: req.ID, ok: true, status: "ok", mode: "make",
			beforeSHA: beforeSHA, afterSHA: afterSHA,
			steps: steps, startTime: startTime,
			denebDir: deps.DenebDir, restartDelayMs: p.RestartDelayMs,
		})
	}
}

// updateResultOpts groups parameters for updateResult.
type updateResultOpts struct {
	reqID          string
	ok             bool
	status         string
	mode           string
	beforeSHA      string
	afterSHA       string
	steps          []updateStep
	startTime      time.Time
	denebDir       string
	restartDelayMs int64
}

func updateResult(opts updateResultOpts) *protocol.ResponseFrame {
	durationMs := time.Since(opts.startTime).Milliseconds()

	// Write restart sentinel on success.
	var sentinel map[string]any
	if opts.ok && opts.denebDir != "" {
		sentinelPath := filepath.Join(opts.denebDir, ".update-sentinel")
		sentinelPayload := map[string]any{
			"updatedAt":  time.Now().Format(time.RFC3339),
			"beforeSHA":  opts.beforeSHA,
			"afterSHA":   opts.afterSHA,
			"durationMs": durationMs,
		}
		data, _ := json.Marshal(sentinelPayload)
		_ = os.WriteFile(sentinelPath, data, 0o644) //nolint:gosec // G306 — world-readable is intentional
		sentinel = map[string]any{
			"path":    sentinelPath,
			"payload": sentinelPayload,
		}
	}

	var restart any
	if opts.ok && opts.restartDelayMs > 0 {
		restart = map[string]any{"delayMs": opts.restartDelayMs}
	}

	result := map[string]any{
		"ok": opts.ok,
		"result": map[string]any{
			"status":     opts.status,
			"mode":       opts.mode,
			"before":     map[string]any{"sha": opts.beforeSHA},
			"after":      map[string]any{"sha": opts.afterSHA},
			"steps":      opts.steps,
			"durationMs": durationMs,
		},
		"restart":  restart,
		"sentinel": sentinel,
	}

	resp, _ := protocol.NewResponseOK(opts.reqID, result)
	return resp
}

func runStep(ctx context.Context, dir, name, cmdName string, args ...string) updateStep {
	start := time.Now()
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	step := updateStep{
		Name:       name,
		Command:    fmt.Sprintf("%s %s", cmdName, strings.Join(args, " ")),
		Cwd:        dir,
		DurationMs: time.Since(start).Milliseconds(),
		Log:        truncateLog(out.String(), 4096),
		OK:         err == nil,
	}
	return step
}

func runGitRev(dir string) string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
