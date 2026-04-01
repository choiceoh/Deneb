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
	watchingBuild bool   // true while polling build status
}

func newBuildWatcher(svc *Service) *BuildWatcher {
	return &BuildWatcher{svc: svc}
}

// OnPushDetected is called when a git push is detected in conversation.
func (bw *BuildWatcher) OnPushDetected(branch string) {
	bw.svc.mu.Lock()
	bw.lastPushAt = time.Now().UnixMilli()
	bw.lastBranch = branch
	bw.watchingBuild = true
	bw.svc.mu.Unlock()

	bw.svc.cfg.Logger.Info("shadow: git push detected, watching build",
		"branch", branch,
	)

	// Start async build check.
	go bw.pollBuildStatus()
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
