package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// defaultGitHubWebhookEvents is the canonical set of events KAIROS monitors.
var defaultGitHubWebhookEvents = []string{
	"push", "pull_request", "issues", "issue_comment",
	"pull_request_review", "create", "delete", "workflow_run",
}

// ToolGitHubWebhook returns a tool that manages GitHub webhook registration
// for the Deneb gateway's POST /webhook/github endpoint (KAIROS integration).
//
// Uses the `gh` CLI under the hood — the user must be authenticated via
// `gh auth login` or a GITHUB_TOKEN environment variable.
func ToolGitHubWebhook() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action     string   `json:"action"`
			Repo       string   `json:"repo"`
			Events     []string `json:"events"`
			WebhookID  int64    `json:"webhook_id"`
			GatewayURL string   `json:"gateway_url"`
		}
		if err := jsonutil.UnmarshalInto("github_webhook params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "status":
			return githubWebhookStatus()

		case "setup":
			return githubWebhookSetup(ctx, p.Repo, p.Events, p.GatewayURL)

		case "list":
			if p.Repo == "" {
				return "", fmt.Errorf("repo is required for list action")
			}
			return githubWebhookList(ctx, p.Repo)

		case "delete":
			if p.Repo == "" {
				return "", fmt.Errorf("repo is required for delete action")
			}
			if p.WebhookID == 0 {
				return "", fmt.Errorf("webhook_id is required for delete action")
			}
			return githubWebhookDelete(ctx, p.Repo, p.WebhookID)

		default:
			return "", fmt.Errorf("unknown action %q — valid: setup, list, delete, status", p.Action)
		}
	}
}

// githubWebhookStatus reports the current Deneb-side webhook configuration.
func githubWebhookStatus() (string, error) {
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	chatID := os.Getenv("GITHUB_WEBHOOK_CHAT_ID")

	var sb strings.Builder
	sb.WriteString("=== Deneb GitHub Webhook Status ===\n")
	sb.WriteString("Endpoint:  POST /webhook/github\n")

	if secret == "" {
		sb.WriteString("Secret:    ⚠️  GITHUB_WEBHOOK_SECRET not set — endpoint is DISABLED\n")
	} else {
		sb.WriteString("Secret:    ✅ configured (GITHUB_WEBHOOK_SECRET)\n")
	}

	if chatID == "" {
		sb.WriteString("Chat ID:   ⚠️  GITHUB_WEBHOOK_CHAT_ID not set — Telegram delivery disabled\n")
	} else {
		sb.WriteString(fmt.Sprintf("Chat ID:   ✅ %s\n", chatID))
	}

	sb.WriteString("\nTo enable: set GITHUB_WEBHOOK_SECRET and GITHUB_WEBHOOK_CHAT_ID in ~/.profile (or ~/.deneb/.env), then restart the gateway.\n")
	sb.WriteString("To register on a GitHub repo: use action=setup with the repo name.\n")
	return sb.String(), nil
}

// githubWebhookSetup registers a webhook on a GitHub repo via `gh api`.
func githubWebhookSetup(ctx context.Context, repo string, events []string, gatewayURL string) (string, error) {
	if repo == "" {
		return "", fmt.Errorf("repo is required for setup action (format: owner/name)")
	}

	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		return "⚠️  GITHUB_WEBHOOK_SECRET is not set. Set it first, then run setup.\n" +
			"The secret must match what you configure in GitHub.", nil
	}

	// Resolve gateway URL: explicit override → DENEB_GATEWAY_URL env → fallback hint.
	if gatewayURL == "" {
		gatewayURL = os.Getenv("DENEB_GATEWAY_URL")
	}
	if gatewayURL == "" {
		return "⚠️  Cannot determine the public gateway URL.\n" +
			"Provide gateway_url explicitly or set DENEB_GATEWAY_URL to the externally reachable URL\n" +
			"(e.g. https://yourserver.example.com). The gateway must be accessible from GitHub's servers.", nil
	}

	webhookURL := strings.TrimRight(gatewayURL, "/") + "/webhook/github"

	if len(events) == 0 {
		events = defaultGitHubWebhookEvents
	}

	// Build the JSON body for gh api.
	eventsJSON, _ := json.Marshal(events)
	body := fmt.Sprintf(`{"name":"web","active":true,"events":%s,"config":{"url":"%s","content_type":"json","secret":"%s","insecure_ssl":"0"}}`,
		string(eventsJSON), webhookURL, secret)

	out, err := ghAPICall(ctx, "POST", fmt.Sprintf("/repos/%s/hooks", repo), body)
	if err != nil {
		return "", fmt.Errorf("gh api failed: %w", err)
	}

	// Parse response to extract webhook ID and confirm URL.
	var resp map[string]any
	if jsonErr := json.Unmarshal([]byte(out), &resp); jsonErr == nil {
		id, _ := resp["id"].(float64)
		pingURL, _ := resp["ping_url"].(string)
		_ = pingURL
		return fmt.Sprintf("✅ Webhook registered on %s\nID: %d\nURL: %s\nEvents: %s\n\nGitHub will verify with a ping — check the gateway logs for '\"event\":\"ping\"'.",
			repo, int(id), webhookURL, strings.Join(events, ", ")), nil
	}

	return out, nil
}

// githubWebhookList lists existing webhooks on a repo.
func githubWebhookList(ctx context.Context, repo string) (string, error) {
	out, err := ghAPICall(ctx, "GET", fmt.Sprintf("/repos/%s/hooks", repo), "")
	if err != nil {
		return "", fmt.Errorf("gh api failed: %w", err)
	}

	var hooks []map[string]any
	if err := json.Unmarshal([]byte(out), &hooks); err != nil {
		return out, nil // return raw on parse error
	}

	if len(hooks) == 0 {
		return fmt.Sprintf("No webhooks found on %s.", repo), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Webhooks on %s:\n\n", repo)
	for _, h := range hooks {
		id, _ := h["id"].(float64)
		active, _ := h["active"].(bool)
		cfg, _ := h["config"].(map[string]any)
		url, _ := cfg["url"].(string)
		events, _ := h["events"].([]any)

		activeStr := "inactive"
		if active {
			activeStr = "active"
		}

		evtStrs := make([]string, 0, len(events))
		for _, e := range events {
			if s, ok := e.(string); ok {
				evtStrs = append(evtStrs, s)
			}
		}

		fmt.Fprintf(&sb, "ID: %d | %s | %s\n  Events: %s\n\n",
			int(id), activeStr, url, strings.Join(evtStrs, ", "))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// githubWebhookDelete removes a webhook by ID.
func githubWebhookDelete(ctx context.Context, repo string, id int64) (string, error) {
	_, err := ghAPICall(ctx, "DELETE", fmt.Sprintf("/repos/%s/hooks/%d", repo, id), "")
	if err != nil {
		return "", fmt.Errorf("gh api failed: %w", err)
	}
	return fmt.Sprintf("✅ Webhook %d deleted from %s.", id, repo), nil
}

// ghAPICall executes a `gh api` command and returns stdout.
func ghAPICall(ctx context.Context, method, path, body string) (string, error) {
	args := []string{"api", "--method", method, path}
	if body != "" {
		args = append(args, "--input", "-")
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	if body != "" {
		cmd.Stdin = strings.NewReader(body)
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w\n%s", err, string(exitErr.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
