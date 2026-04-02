package shadow

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// BuildWatcher monitors git push events and checks CI/build status.
type BuildWatcher struct {
	svc *Service

	// State (guarded by svc.mu).
	lastPushAt    int64  // unix ms
	lastBranch    string // branch that was pushed
	lastRepo      string // repo that was pushed to (set by webhook path)
	pushSource    string // "webhook" or "text_detect"
	watchingBuild bool   // true while polling build status
	ciReceived    chan struct{} // closed when OnCIResult is called
}

func newBuildWatcher(svc *Service) *BuildWatcher {
	return &BuildWatcher{svc: svc}
}

// OnPushDetected is called when a git push is detected in conversation text.
// This is the fallback path when no GitHub webhook is configured.
func (bw *BuildWatcher) OnPushDetected(branch string) {
	bw.svc.mu.Lock()
	bw.lastPushAt = time.Now().UnixMilli()
	bw.lastBranch = branch
	bw.pushSource = "text_detect"
	bw.watchingBuild = true
	bw.svc.mu.Unlock()

	bw.svc.cfg.Logger.Info("shadow: git push detected via text, watching build",
		"branch", branch,
		"source", "text_detect",
	)

	// Start async build check (local polling fallback).
	go bw.pollBuildStatus()
}

// OnGitHubPush is called when a real GitHub push webhook is received.
// This is the preferred path over text-based OnPushDetected.
func (bw *BuildWatcher) OnGitHubPush(branch string, commitCount int, repo string) {
	bw.svc.mu.Lock()
	bw.lastPushAt = time.Now().UnixMilli()
	bw.lastBranch = branch
	bw.lastRepo = repo
	bw.pushSource = "webhook"
	bw.watchingBuild = true
	bw.ciReceived = make(chan struct{})
	bw.svc.mu.Unlock()

	bw.svc.cfg.Logger.Info("shadow: GitHub push webhook received, waiting for CI",
		"branch", branch,
		"commits", commitCount,
		"repo", repo,
	)

	// Wait for CI result from workflow_run webhook; fall back to local check after 10min.
	go bw.waitForCIOrFallback()
}

// OnCIResult is called when a workflow_run completed event arrives via webhook.
func (bw *BuildWatcher) OnCIResult(branch string, passed bool, workflow string) {
	bw.svc.mu.Lock()
	bw.watchingBuild = false
	// Signal waitForCIOrFallback that CI result arrived.
	if bw.ciReceived != nil {
		select {
		case <-bw.ciReceived:
			// already closed
		default:
			close(bw.ciReceived)
		}
	}
	bw.svc.mu.Unlock()

	status := "CI 성공"
	if !passed {
		status = fmt.Sprintf("CI 실패: %s", workflow)
	}

	bw.svc.emit(ShadowEvent{Type: "build_status", Payload: map[string]any{
		"branch":   branch,
		"status":   status,
		"source":   "github_webhook",
		"workflow": workflow,
	}})

	if !passed {
		bw.notifyBuildFailure(status)
	}
}

// waitForCIOrFallback waits up to 10 minutes for a CI result from webhook.
// If no result arrives, falls back to local build polling.
func (bw *BuildWatcher) waitForCIOrFallback() {
	bw.svc.mu.Lock()
	ch := bw.ciReceived
	bw.svc.mu.Unlock()

	if ch == nil {
		return
	}

	select {
	case <-ch:
		// CI result arrived via OnCIResult — nothing more to do.
		return
	case <-time.After(10 * time.Minute):
		// No CI webhook received — repo may not have CI configured.
		bw.svc.cfg.Logger.Info("shadow: no CI webhook received after 10min, falling back to local check")
		bw.pollBuildStatus()
	case <-bw.svc.svcCtx.Done():
		return
	}
}

// pollBuildStatus checks build status periodically after a push.
func (bw *BuildWatcher) pollBuildStatus() {
	// Check at 30s, 1m, 2m, 5m intervals.
	intervals := []time.Duration{
		30 * time.Second,
		30 * time.Second,
		60 * time.Second,
		180 * time.Second,
	}

	for _, wait := range intervals {
		select {
		case <-bw.svc.svcCtx.Done():
			return
		case <-time.After(wait):
		}

		status := bw.checkLocalBuildStatus()
		if status != "" {
			bw.svc.mu.Lock()
			bw.watchingBuild = false
			branch := bw.lastBranch
			bw.svc.mu.Unlock()

			bw.svc.emit(ShadowEvent{Type: "build_status", Payload: map[string]any{
				"branch": branch,
				"status": status,
			}})

			if strings.Contains(status, "실패") || strings.Contains(status, "fail") {
				bw.notifyBuildFailure(status)
			}
			return
		}
	}

	bw.svc.mu.Lock()
	bw.watchingBuild = false
	bw.svc.mu.Unlock()
}

// checkLocalBuildStatus runs a quick local build check.
func (bw *BuildWatcher) checkLocalBuildStatus() string {
	// Check if make check passes (lightweight).
	ctx := bw.svc.svcCtx
	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = resolveGatewayDir()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("빌드 검증 실패: %s", truncate(string(output), 200))
	}
	return "빌드 검증 통과"
}

func (bw *BuildWatcher) notifyBuildFailure(status string) {
	bw.svc.mu.Lock()
	notifier := bw.svc.cfg.Notifier
	branch := bw.lastBranch
	bw.svc.mu.Unlock()
	if notifier == nil {
		return
	}

	msg := fmt.Sprintf("🔴 빌드 실패 알림\n브랜치: %s\n상태: %s", branch, status)
	go func() {
		ctx, cancel := bw.svc.notifyCtx()
		defer cancel()
		if err := notifier.Notify(ctx, msg); err != nil {
			bw.svc.cfg.Logger.Warn("shadow: build notification failed", "error", err)
		}
	}()
}

// pushPatterns detect git push mentions in conversation.
var pushPatterns = []string{
	"git push", "푸시", "push 했", "push 완료", "pushed",
	"push -u", "push origin",
}

// detectPush checks if a message mentions a git push event.
func detectPush(content string) (branch string, detected bool) {
	lower := strings.ToLower(content)
	for _, p := range pushPatterns {
		if strings.Contains(lower, p) {
			// Try to extract branch name.
			branch = extractBranchName(content)
			return branch, true
		}
	}
	return "", false
}

// extractBranchName attempts to find a branch name from push output or command.
func extractBranchName(content string) string {
	// Look for "origin <branch>" or "branch '<name>'"
	parts := strings.Fields(content)
	for i, p := range parts {
		if p == "origin" && i+1 < len(parts) {
			candidate := strings.Trim(parts[i+1], "'\"")
			if candidate != "" && !strings.HasPrefix(candidate, "-") {
				return candidate
			}
		}
	}
	// Look for -> pattern in push output (e.g., "main -> main").
	for i, p := range parts {
		if p == "->" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "unknown"
}

func resolveGatewayDir() string {
	// Best-effort resolution of gateway-go directory.
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "."
	}
	return strings.TrimSpace(string(output)) + "/gateway-go"
}
