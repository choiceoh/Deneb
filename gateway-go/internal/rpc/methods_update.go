package rpc

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

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// UpdateDeps holds dependencies for the update.run RPC method.
type UpdateDeps struct {
	// RepoDir is the repository root directory for git operations.
	// Defaults to the current working directory if empty.
	RepoDir string
	// DenebDir is the deneb config directory (~/.deneb) for sentinel file.
	DenebDir string
}

// RegisterUpdateMethods registers the update.run RPC method.
func RegisterUpdateMethods(d *Dispatcher, deps UpdateDeps) {
	d.Register("update.run", updateRun(deps))
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

func updateRun(deps UpdateDeps) HandlerFunc {
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
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "cannot determine working directory: "+err.Error()))
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
			return updateResult(req.ID, false, "error", "git", beforeSHA, beforeSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
		}

		// Step 3: Get new git SHA (after).
		afterSHA := runGitRev(repoDir)

		// Step 4: Rebuild via make.
		buildStep := runStep(updateCtx, repoDir, "make all", "make", "all")
		steps = append(steps, buildStep)
		if !buildStep.OK {
			return updateResult(req.ID, false, "error", "make", beforeSHA, afterSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
		}

		return updateResult(req.ID, true, "ok", "make", beforeSHA, afterSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
	}
}

func updateResult(reqID string, ok bool, status, mode, beforeSHA, afterSHA string, steps []updateStep, startTime time.Time, denebDir string, restartDelayMs int64) *protocol.ResponseFrame {
	durationMs := time.Since(startTime).Milliseconds()

	// Write restart sentinel on success.
	var sentinel map[string]any
	if ok && denebDir != "" {
		sentinelPath := filepath.Join(denebDir, ".update-sentinel")
		sentinelPayload := map[string]any{
			"updatedAt":  time.Now().Format(time.RFC3339),
			"beforeSHA":  beforeSHA,
			"afterSHA":   afterSHA,
			"durationMs": durationMs,
		}
		data, _ := json.Marshal(sentinelPayload)
		_ = os.WriteFile(sentinelPath, data, 0644)
		sentinel = map[string]any{
			"path":    sentinelPath,
			"payload": sentinelPayload,
		}
	}

	var restart any
	if ok && restartDelayMs > 0 {
		restart = map[string]any{"delayMs": restartDelayMs}
	}

	result := map[string]any{
		"ok": ok,
		"result": map[string]any{
			"status":     status,
			"mode":       mode,
			"before":     map[string]any{"sha": beforeSHA},
			"after":      map[string]any{"sha": afterSHA},
			"steps":      steps,
			"durationMs": durationMs,
		},
		"restart":  restart,
		"sentinel": sentinel,
	}

	resp, _ := protocol.NewResponseOK(reqID, result)
	return resp
}

func runStep(ctx context.Context, dir, name string, cmdName string, args ...string) updateStep {
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
	cmd := exec.Command("git", "rev-parse", "HEAD")
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
