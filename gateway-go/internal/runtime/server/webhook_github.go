// Package server — webhook_github.go handles inbound GitHub webhook events.
//
// Endpoint: POST /webhook/github
//
// Required env vars:
//   - GITHUB_WEBHOOK_SECRET   — shared HMAC-SHA256 secret set in GitHub repo settings
//   - GITHUB_WEBHOOK_CHAT_ID  — Telegram chat ID to deliver notifications to
//
// Supported events: ping, push, pull_request, issues, issue_comment,
// pull_request_review, create, delete, workflow_run.
//
// Each event is:
//  1. HMAC-SHA256 verified against the shared secret.
//  2. Formatted as a Korean Telegram notification and delivered directly.
//  3. Propagated to the hooks registry as github.webhook so user-defined
//     shell hooks can react to it.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
)

const (
	githubWebhookMaxBodyBytes = 10 * 1024 * 1024 // 10 MB (GitHub sends large push payloads)
	githubWebhookTimeout      = 10 * time.Second
)

// GitHubWebhookConfig holds runtime configuration for the GitHub webhook handler.
// Fields are resolved from environment variables at server startup.
type GitHubWebhookConfig struct {
	// Secret is the HMAC-SHA256 shared secret configured in GitHub repo/org settings.
	Secret string
	// ChatID is the Telegram chat ID to deliver event notifications to.
	ChatID string
}

// GitHubWebhookConfigFromEnv reads webhook config from environment variables.
// Returns nil when GITHUB_WEBHOOK_SECRET is unset (webhook disabled).
func GitHubWebhookConfigFromEnv() *GitHubWebhookConfig {
	secret := strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET"))
	if secret == "" {
		return nil
	}
	return &GitHubWebhookConfig{
		Secret: secret,
		ChatID: strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_CHAT_ID")),
	}
}

// handleGitHubWebhook processes POST /webhook/github.
//
// The server's githubWebhookCfg field controls whether this handler is active;
// it is set once during New() and never mutated afterwards.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	cfg := s.githubWebhookCfg
	if cfg == nil {
		writeText(w, http.StatusNotFound, "GitHub webhook not configured")
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	// ── 1. Read body (bounded) ────────────────────────────────────────────
	body, err := io.ReadAll(io.LimitReader(r.Body, githubWebhookMaxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "failed to read body"})
		return
	}

	// ── 2. Verify HMAC-SHA256 signature ──────────────────────────────────
	sig := r.Header.Get("X-Hub-Signature-256")
	if !verifyGitHubSignature(cfg.Secret, body, sig) {
		s.logger.Warn("github webhook signature mismatch",
			"ip", resolveClientIP(r),
			"sig", sig,
		)
		writeText(w, http.StatusUnauthorized, "Invalid signature")
		return
	}

	eventType := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GitHub-Event")))
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	// ── 3. Parse payload ──────────────────────────────────────────────────
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
		return
	}

	s.logger.Info("github webhook received",
		"event", eventType,
		"delivery", deliveryID,
		"bodyBytes", len(body),
	)

	// Acknowledge immediately — GitHub expects a 2xx within a few seconds.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	// ── 4. Handle asynchronously so we never block GitHub's delivery timeout ──
	s.safeGo("github-webhook:"+eventType, func() {
		ctx, cancel := context.WithTimeout(context.Background(), githubWebhookTimeout)
		defer cancel()

		msg := formatGitHubEventKorean(eventType, payload)
		if msg == "" {
			return // unhandled/uninteresting event — skip delivery
		}

		// Deliver to Telegram if a chat ID is configured.
		if cfg.ChatID != "" && s.telegramPlug != nil {
			out := telegram.OutboundMessage{
				To:   cfg.ChatID,
				Text: msg,
			}
			if err := s.telegramPlug.SendMessage(ctx, out); err != nil {
				s.logger.Warn("github webhook telegram delivery failed",
					"event", eventType,
					"chatID", cfg.ChatID,
					"error", err,
				)
			}
		} else if cfg.ChatID == "" {
			s.logger.Debug("github webhook: GITHUB_WEBHOOK_CHAT_ID not set, skipping telegram delivery")
		}

		// Fire github.webhook internal hook.
		if s.internalHooks != nil {
			env := map[string]string{
				"GITHUB_EVENT":       eventType,
				"GITHUB_DELIVERY":    deliveryID,
				"GITHUB_REPO":        extractGitHubRepo(payload),
				"GITHUB_ACTOR":       extractGitHubActor(payload),
				"DENEB_HOOK_CHANNEL": "github",
			}
			s.internalHooks.TriggerFromEvent(ctx, hooks.EventGitHubWebhook, "", env)
		}
	})
}

// verifyGitHubSignature checks the X-Hub-Signature-256 header against the payload.
// GitHub format: "sha256=<hex-digest>"
func verifyGitHubSignature(secret string, body []byte, sig string) bool {
	if secret == "" {
		// No secret configured — accept all (development only).
		return true
	}
	const prefix = "sha256="
	if !strings.HasPrefix(sig, prefix) {
		return false
	}
	want, err := hex.DecodeString(sig[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// ── Payload field helpers ────────────────────────────────────────────────────

func extractGitHubRepo(p map[string]any) string {
	if repo, ok := p["repository"].(map[string]any); ok {
		if name, ok := repo["full_name"].(string); ok {
			return name
		}
	}
	return ""
}

func extractGitHubActor(p map[string]any) string {
	// sender is the authenticated actor for all event types.
	if sender, ok := p["sender"].(map[string]any); ok {
		if login, ok := sender["login"].(string); ok {
			return login
		}
	}
	return ""
}

func strField(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

// ── Korean message formatters ────────────────────────────────────────────────

// formatGitHubEventKorean returns a Korean Telegram notification for the event.
// Returns "" for events that don't warrant a notification.
func formatGitHubEventKorean(eventType string, p map[string]any) string {
	repo := extractGitHubRepo(p)
	actor := extractGitHubActor(p)

	repoLabel := repo
	if repoLabel == "" {
		repoLabel = "(알 수 없는 레포)"
	}
	actorLabel := actor
	if actorLabel == "" {
		actorLabel = "(알 수 없는 사용자)"
	}

	switch eventType {
	case "ping":
		return fmt.Sprintf("🔔 GitHub webhook 연결 확인\n레포: %s\nZen: %s", repoLabel, strField(p, "zen"))

	case "push":
		return formatPushEvent(p, repoLabel, actorLabel)

	case "pull_request":
		return formatPREvent(p, repoLabel, actorLabel)

	case "issues":
		return formatIssueEvent(p, repoLabel, actorLabel)

	case "issue_comment":
		return formatIssueCommentEvent(p, repoLabel, actorLabel)

	case "pull_request_review":
		return formatPRReviewEvent(p, repoLabel, actorLabel)

	case "create":
		return formatCreateEvent(p, repoLabel, actorLabel)

	case "delete":
		return formatDeleteEvent(p, repoLabel, actorLabel)

	case "workflow_run":
		return formatWorkflowRunEvent(p, repoLabel, actorLabel)

	default:
		return "" // unhandled event type — skip delivery
	}
}

func formatPushEvent(p map[string]any, repo, actor string) string {
	ref := strField(p, "ref")
	branch := strings.TrimPrefix(ref, "refs/heads/")

	commits, _ := p["commits"].([]any)
	count := len(commits)

	var sb strings.Builder
	fmt.Fprintf(&sb, "📦 %s — push\n", repo)
	fmt.Fprintf(&sb, "브랜치: %s | 커밋: %d개 | 작성: %s\n", branch, count, actor)

	// Show up to 5 commit summaries.
	for i, c := range commits {
		if i >= 5 {
			fmt.Fprintf(&sb, "  ... 외 %d개\n", count-5)
			break
		}
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		msg := strField(cm, "message")
		// Trim multi-line commit messages to the subject line.
		if nl := strings.IndexByte(msg, '\n'); nl >= 0 {
			msg = msg[:nl]
		}
		if len(msg) > 72 {
			msg = msg[:69] + "..."
		}
		id := strField(cm, "id")
		if len(id) > 7 {
			id = id[:7]
		}
		fmt.Fprintf(&sb, "  %s %s\n", id, msg)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatPREvent(p map[string]any, repo, actor string) string {
	action := strField(p, "action")
	pr, _ := p["pull_request"].(map[string]any)
	if pr == nil {
		return ""
	}
	title := strField(pr, "title")
	number, _ := pr["number"].(float64)
	url := strField(pr, "html_url")

	actionKo := prActionKorean(action)
	if actionKo == "" {
		return "" // uninteresting sub-action (labeled, unlabeled, etc.)
	}

	return fmt.Sprintf("🔀 PR #%d %s — %s\n%s\n작성: %s\n%s",
		int(number), actionKo, repo, title, actor, url)
}

func prActionKorean(action string) string {
	switch action {
	case "opened":
		return "열림"
	case "closed":
		return "닫힘"
	case "merged":
		return "병합됨"
	case "reopened":
		return "다시 열림"
	case "ready_for_review":
		return "리뷰 준비됨"
	case "review_requested":
		return "리뷰 요청됨"
	default:
		return ""
	}
}

func formatIssueEvent(p map[string]any, repo, actor string) string {
	action := strField(p, "action")
	issue, _ := p["issue"].(map[string]any)
	if issue == nil {
		return ""
	}
	title := strField(issue, "title")
	number, _ := issue["number"].(float64)
	url := strField(issue, "html_url")

	actionKo := issueActionKorean(action)
	if actionKo == "" {
		return ""
	}

	return fmt.Sprintf("🐛 이슈 #%d %s — %s\n%s\n작성: %s\n%s",
		int(number), actionKo, repo, title, actor, url)
}

func issueActionKorean(action string) string {
	switch action {
	case "opened":
		return "열림"
	case "closed":
		return "닫힘"
	case "reopened":
		return "다시 열림"
	case "assigned":
		return "담당자 지정됨"
	default:
		return ""
	}
}

func formatIssueCommentEvent(p map[string]any, repo, actor string) string {
	action := strField(p, "action")
	if action != "created" {
		return ""
	}
	issue, _ := p["issue"].(map[string]any)
	if issue == nil {
		return ""
	}
	number, _ := issue["number"].(float64)
	title := strField(issue, "title")
	comment, _ := p["comment"].(map[string]any)
	body := strField(comment, "body")
	if len(body) > 200 {
		body = body[:197] + "..."
	}
	url := strField(comment, "html_url")

	return fmt.Sprintf("💬 이슈 #%d 댓글 — %s\n%s\n%s\n작성: %s\n%s",
		int(number), repo, title, body, actor, url)
}

func formatPRReviewEvent(p map[string]any, repo, actor string) string {
	action := strField(p, "action")
	if action != "submitted" {
		return ""
	}
	review, _ := p["review"].(map[string]any)
	if review == nil {
		return ""
	}
	state := strings.ToLower(strField(review, "state"))
	pr, _ := p["pull_request"].(map[string]any)
	number, _ := pr["number"].(float64)
	title := strField(pr, "title")
	url := strField(review, "html_url")

	stateKo := reviewStateKorean(state)

	return fmt.Sprintf("👀 PR #%d 리뷰 %s — %s\n%s\n검토: %s\n%s",
		int(number), stateKo, repo, title, actor, url)
}

func reviewStateKorean(state string) string {
	switch state {
	case "approved":
		return "승인됨 ✅"
	case "changes_requested":
		return "수정 요청 🔄"
	case "commented":
		return "댓글 달림 💬"
	default:
		return state
	}
}

func formatCreateEvent(p map[string]any, repo, actor string) string {
	refType := strField(p, "ref_type")
	ref := strField(p, "ref")
	switch refType {
	case "branch":
		return fmt.Sprintf("🌿 브랜치 생성 — %s\n브랜치: %s | 작성: %s", repo, ref, actor)
	case "tag":
		return fmt.Sprintf("🏷️ 태그 생성 — %s\n태그: %s | 작성: %s", repo, ref, actor)
	default:
		return ""
	}
}

func formatDeleteEvent(p map[string]any, repo, actor string) string {
	refType := strField(p, "ref_type")
	ref := strField(p, "ref")
	switch refType {
	case "branch":
		return fmt.Sprintf("🗑️ 브랜치 삭제 — %s\n브랜치: %s | 작성: %s", repo, ref, actor)
	case "tag":
		return fmt.Sprintf("🗑️ 태그 삭제 — %s\n태그: %s | 작성: %s", repo, ref, actor)
	default:
		return ""
	}
}

func formatWorkflowRunEvent(p map[string]any, repo, actor string) string {
	action := strField(p, "action")
	if action != "completed" {
		return ""
	}
	run, _ := p["workflow_run"].(map[string]any)
	if run == nil {
		return ""
	}
	conclusion := strings.ToLower(strField(run, "conclusion"))
	name := strField(run, "name")
	branch := strField(run, "head_branch")
	url := strField(run, "html_url")

	icon, conclusionKo := workflowConclusionKorean(conclusion)
	return fmt.Sprintf("%s CI %s — %s\n워크플로: %s | 브랜치: %s\n%s",
		icon, conclusionKo, repo, name, branch, url)
}

func workflowConclusionKorean(conclusion string) (icon, label string) {
	switch conclusion {
	case "success":
		return "✅", "성공"
	case "failure":
		return "❌", "실패"
	case "cancelled":
		return "⏹️", "취소됨"
	case "skipped":
		return "⏭️", "건너뜀"
	default:
		return "⚙️", conclusion
	}
}
