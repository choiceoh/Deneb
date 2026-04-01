package shadow

import (
	"fmt"
	"strings"
	"time"
)

// GitHubEventTracker tracks GitHub webhook events for enriching shadow monitoring.
// All state is guarded by svc.mu (following the shadow module pattern).
type GitHubEventTracker struct {
	svc *Service

	// State (guarded by svc.mu).
	recentEvents []GitHubEventRecord // ring buffer, capped at maxGitHubEvents
	pushCount    int                 // pushes since last digest
	prActivity   []PRActivityRecord  // PR events since last digest
	ciRuns       []CIRunRecord       // CI runs since last digest
	lastEventAt  int64               // unix ms
}

func newGitHubTracker(svc *Service) *GitHubEventTracker {
	return &GitHubEventTracker{svc: svc}
}

// OnEvent is the main dispatch entry point called by Service.OnGitHubEvent.
// It records the event and fans out to peer sub-modules.
func (gt *GitHubEventTracker) OnEvent(eventType string, payload map[string]any) {
	now := time.Now().UnixMilli()
	repo := ghPayloadRepo(payload)
	actor := ghPayloadActor(payload)

	// Build a Korean one-liner summary for the event log.
	summary := formatEventSummary(eventType, payload, repo, actor)
	if summary == "" {
		return // uninteresting event (e.g. ping)
	}

	record := GitHubEventRecord{
		EventType: eventType,
		Repo:      repo,
		Actor:     actor,
		Summary:   summary,
		Ts:        now,
	}

	gt.svc.mu.Lock()
	gt.lastEventAt = now
	gt.recentEvents = append(gt.recentEvents, record)
	if len(gt.recentEvents) > maxGitHubEvents {
		gt.recentEvents = gt.recentEvents[len(gt.recentEvents)-maxGitHubEvents:]
	}
	gt.svc.mu.Unlock()

	// Dispatch to peer modules based on event type.
	switch eventType {
	case "push":
		gt.handlePush(payload, repo)
	case "workflow_run":
		gt.handleWorkflowRun(payload)
	case "pull_request":
		gt.handlePullRequest(payload, repo, actor, now)
	case "issue_comment", "pull_request_review":
		gt.handlePRFeedback(eventType, payload, repo, actor, now)
	}

	gt.svc.emit(ShadowEvent{Type: "github_event", Payload: record})
}

// handlePush dispatches push events to BuildWatcher.
func (gt *GitHubEventTracker) handlePush(payload map[string]any, repo string) {
	ref := ghPayloadStr(payload, "ref")
	branch := strings.TrimPrefix(ref, "refs/heads/")
	commits, _ := payload["commits"].([]any)

	gt.svc.mu.Lock()
	gt.pushCount++
	gt.svc.mu.Unlock()

	gt.svc.buildWatcher.OnGitHubPush(branch, len(commits), repo)
}

// handleWorkflowRun dispatches CI results to BuildWatcher and ErrorLearner.
func (gt *GitHubEventTracker) handleWorkflowRun(payload map[string]any) {
	action := ghPayloadStr(payload, "action")
	if action != "completed" {
		return
	}
	run, _ := payload["workflow_run"].(map[string]any)
	if run == nil {
		return
	}

	conclusion := strings.ToLower(ghPayloadStr(run, "conclusion"))
	branch := ghPayloadStr(run, "head_branch")
	url := ghPayloadStr(run, "html_url")
	name := ghPayloadStr(run, "name")

	ciRecord := CIRunRecord{
		Workflow:   name,
		Branch:     branch,
		Conclusion: conclusion,
		URL:        url,
		Ts:         time.Now().UnixMilli(),
	}

	gt.svc.mu.Lock()
	gt.ciRuns = append(gt.ciRuns, ciRecord)
	if len(gt.ciRuns) > maxCIRuns {
		gt.ciRuns = gt.ciRuns[len(gt.ciRuns)-maxCIRuns:]
	}
	gt.svc.mu.Unlock()

	switch conclusion {
	case "failure":
		gt.svc.errorLearner.OnCIFailure(name, branch, url)
		gt.svc.buildWatcher.OnCIResult(branch, false, name)
	case "success":
		gt.svc.buildWatcher.OnCIResult(branch, true, name)
	}
}

// handlePullRequest dispatches PR events to ContextPrefetcher.
func (gt *GitHubEventTracker) handlePullRequest(payload map[string]any, repo, actor string, now int64) {
	action := ghPayloadStr(payload, "action")
	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		return
	}

	number, _ := pr["number"].(float64)
	title := ghPayloadStr(pr, "title")
	url := ghPayloadStr(pr, "html_url")

	record := PRActivityRecord{
		Number: int(number),
		Title:  title,
		Action: action,
		Actor:  actor,
		URL:    url,
		Ts:     now,
	}

	gt.svc.mu.Lock()
	gt.prActivity = append(gt.prActivity, record)
	if len(gt.prActivity) > maxPRActivity {
		gt.prActivity = gt.prActivity[len(gt.prActivity)-maxPRActivity:]
	}
	gt.svc.mu.Unlock()

	// Trigger context prefetch for new or review-requested PRs.
	if action == "opened" || action == "review_requested" || action == "ready_for_review" {
		gt.svc.contextPrefetcher.OnPRActivity(pr)
	}
}

// handlePRFeedback records PR review/comment activity.
func (gt *GitHubEventTracker) handlePRFeedback(eventType string, payload map[string]any, repo, actor string, now int64) {
	var number float64
	var title, url string

	switch eventType {
	case "pull_request_review":
		if ghPayloadStr(payload, "action") != "submitted" {
			return
		}
		pr, _ := payload["pull_request"].(map[string]any)
		if pr == nil {
			return
		}
		number, _ = pr["number"].(float64)
		title = ghPayloadStr(pr, "title")
		review, _ := payload["review"].(map[string]any)
		url = ghPayloadStr(review, "html_url")
	case "issue_comment":
		if ghPayloadStr(payload, "action") != "created" {
			return
		}
		// Only track if this is a PR comment (has pull_request key).
		issue, _ := payload["issue"].(map[string]any)
		if issue == nil {
			return
		}
		if _, hasPR := issue["pull_request"]; !hasPR {
			return
		}
		number, _ = issue["number"].(float64)
		title = ghPayloadStr(issue, "title")
		comment, _ := payload["comment"].(map[string]any)
		url = ghPayloadStr(comment, "html_url")
	}

	if number == 0 {
		return
	}

	record := PRActivityRecord{
		Number: int(number),
		Title:  title,
		Action: eventType,
		Actor:  actor,
		URL:    url,
		Ts:     now,
	}

	gt.svc.mu.Lock()
	gt.prActivity = append(gt.prActivity, record)
	if len(gt.prActivity) > maxPRActivity {
		gt.prActivity = gt.prActivity[len(gt.prActivity)-maxPRActivity:]
	}
	gt.svc.mu.Unlock()
}

// GetActivitySummary returns the current GitHub activity summary for RPC.
func (gt *GitHubEventTracker) GetActivitySummary() *GitHubActivitySummary {
	gt.svc.mu.Lock()
	defer gt.svc.mu.Unlock()

	events := make([]GitHubEventRecord, len(gt.recentEvents))
	copy(events, gt.recentEvents)

	prs := make([]PRActivityRecord, len(gt.prActivity))
	copy(prs, gt.prActivity)

	cis := make([]CIRunRecord, len(gt.ciRuns))
	copy(cis, gt.ciRuns)

	return &GitHubActivitySummary{
		RecentEvents: events,
		TotalPushes:  gt.pushCount,
		PRActivity:   prs,
		CIRuns:       cis,
		LastEventAt:  gt.lastEventAt,
	}
}

// GetRecentEvents returns a copy of recent events for the RPC.
func (gt *GitHubEventTracker) GetRecentEvents() []GitHubEventRecord {
	gt.svc.mu.Lock()
	defer gt.svc.mu.Unlock()
	result := make([]GitHubEventRecord, len(gt.recentEvents))
	copy(result, gt.recentEvents)
	return result
}

// FormatDigestSection returns a Korean summary of GitHub activity since the last digest.
// Returns "" if there was no activity.
func (gt *GitHubEventTracker) FormatDigestSection() string {
	gt.svc.mu.Lock()
	pushes := gt.pushCount
	prs := len(gt.prActivity)
	ciSuccess, ciFail := 0, 0
	for _, ci := range gt.ciRuns {
		switch ci.Conclusion {
		case "success":
			ciSuccess++
		case "failure":
			ciFail++
		}
	}
	gt.svc.mu.Unlock()

	if pushes == 0 && prs == 0 && ciSuccess == 0 && ciFail == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("🐙 GitHub 활동 요약")
	if pushes > 0 {
		fmt.Fprintf(&sb, "\n  • 푸시: %d건", pushes)
	}
	if prs > 0 {
		fmt.Fprintf(&sb, "\n  • PR 활동: %d건", prs)
	}
	if ciSuccess > 0 || ciFail > 0 {
		fmt.Fprintf(&sb, "\n  • CI: ✅ %d건 성공", ciSuccess)
		if ciFail > 0 {
			fmt.Fprintf(&sb, ", ❌ %d건 실패", ciFail)
		}
	}
	return sb.String()
}

// ResetDigestCounters clears per-period accumulators after a digest is sent.
func (gt *GitHubEventTracker) ResetDigestCounters() {
	gt.svc.mu.Lock()
	gt.pushCount = 0
	gt.prActivity = gt.prActivity[:0]
	gt.ciRuns = gt.ciRuns[:0]
	gt.svc.mu.Unlock()
}

// --- Payload helpers (duplicated from webhook_github.go since different package) ---

func ghPayloadRepo(p map[string]any) string {
	if repo, ok := p["repository"].(map[string]any); ok {
		if name, ok := repo["full_name"].(string); ok {
			return name
		}
	}
	return ""
}

func ghPayloadActor(p map[string]any) string {
	if sender, ok := p["sender"].(map[string]any); ok {
		if login, ok := sender["login"].(string); ok {
			return login
		}
	}
	return ""
}

func ghPayloadStr(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

// formatEventSummary returns a Korean one-liner for the event log.
// Returns "" for events that should be skipped (e.g., ping).
func formatEventSummary(eventType string, p map[string]any, repo, actor string) string {
	switch eventType {
	case "push":
		ref := ghPayloadStr(p, "ref")
		branch := strings.TrimPrefix(ref, "refs/heads/")
		commits, _ := p["commits"].([]any)
		return fmt.Sprintf("📦 %s에 %s 브랜치 푸시 (%d커밋, %s)", repo, branch, len(commits), actor)

	case "pull_request":
		action := ghPayloadStr(p, "action")
		pr, _ := p["pull_request"].(map[string]any)
		if pr == nil {
			return ""
		}
		number, _ := pr["number"].(float64)
		title := ghPayloadStr(pr, "title")
		return fmt.Sprintf("🔀 PR #%d %s: %s (%s)", int(number), action, truncate(title, 40), actor)

	case "workflow_run":
		action := ghPayloadStr(p, "action")
		if action != "completed" {
			return ""
		}
		run, _ := p["workflow_run"].(map[string]any)
		if run == nil {
			return ""
		}
		conclusion := ghPayloadStr(run, "conclusion")
		name := ghPayloadStr(run, "name")
		branch := ghPayloadStr(run, "head_branch")
		return fmt.Sprintf("⚙️ CI %s: %s/%s (%s)", conclusion, name, branch, actor)

	case "issues":
		action := ghPayloadStr(p, "action")
		issue, _ := p["issue"].(map[string]any)
		if issue == nil {
			return ""
		}
		number, _ := issue["number"].(float64)
		title := ghPayloadStr(issue, "title")
		return fmt.Sprintf("🐛 이슈 #%d %s: %s (%s)", int(number), action, truncate(title, 40), actor)

	case "issue_comment":
		issue, _ := p["issue"].(map[string]any)
		if issue == nil {
			return ""
		}
		number, _ := issue["number"].(float64)
		return fmt.Sprintf("💬 #%d 댓글 (%s)", int(number), actor)

	case "pull_request_review":
		pr, _ := p["pull_request"].(map[string]any)
		if pr == nil {
			return ""
		}
		number, _ := pr["number"].(float64)
		review, _ := p["review"].(map[string]any)
		state := ghPayloadStr(review, "state")
		return fmt.Sprintf("👀 PR #%d 리뷰 %s (%s)", int(number), state, actor)

	case "create":
		refType := ghPayloadStr(p, "ref_type")
		ref := ghPayloadStr(p, "ref")
		return fmt.Sprintf("🌿 %s %s 생성 (%s)", refType, ref, actor)

	case "delete":
		refType := ghPayloadStr(p, "ref_type")
		ref := ghPayloadStr(p, "ref")
		return fmt.Sprintf("🗑️ %s %s 삭제 (%s)", refType, ref, actor)

	default:
		return ""
	}
}
