package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// --- web_search tool ---

func webSearchToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Number of results to return (default: 5)",
			},
		},
		"required": []string{"query"},
	}
}

func toolWebSearch() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query string `json:"query"`
			Count int    `json:"count"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid web_search params: %w", err)
		}
		if p.Query == "" {
			return "", fmt.Errorf("query is required")
		}
		if p.Count <= 0 {
			p.Count = 5
		}

		// Try Brave Search API if key is available.
		braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
		if braveKey == "" {
			braveKey = os.Getenv("BRAVE_API_KEY")
		}
		if braveKey != "" {
			return braveWebSearch(ctx, braveKey, p.Query, p.Count)
		}

		// Fallback: use DuckDuckGo instant answers (no API key needed).
		return duckDuckGoSearch(ctx, p.Query)
	}
}

func braveWebSearch(ctx context.Context, apiKey, query string, count int) (string, error) {
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("brave search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Sprintf("Brave Search returned HTTP %d", resp.StatusCode), nil
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse brave response: %w", err)
	}

	var sb strings.Builder
	for i, r := range result.Web.Results {
		fmt.Fprintf(&sb, "%d. **%s**\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	if sb.Len() == 0 {
		return "No results found.", nil
	}
	return sb.String(), nil
}

func duckDuckGoSearch(ctx context.Context, query string) (string, error) {
	reqURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Deneb-Gateway/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("duckduckgo search failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Abstract     string `json:"Abstract"`
		AbstractURL  string `json:"AbstractURL"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse duckduckgo response: %w", err)
	}

	var sb strings.Builder
	if result.Abstract != "" {
		fmt.Fprintf(&sb, "**Summary:** %s\nSource: %s\n\n", result.Abstract, result.AbstractURL)
	}
	for i, topic := range result.RelatedTopics {
		if i >= 5 {
			break
		}
		if topic.Text != "" {
			fmt.Fprintf(&sb, "- %s\n  %s\n", topic.Text, topic.FirstURL)
		}
	}
	if sb.Len() == 0 {
		return "No instant answers available. Try web_fetch with a search engine URL for full results.", nil
	}
	return sb.String(), nil
}

// --- cron tool ---

func cronToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Cron action: status, list, add, update, remove, run, wake",
			},
			"jobId": map[string]any{
				"type":        "string",
				"description": "Job ID for update/remove/run actions",
			},
			"job": map[string]any{
				"type":        "object",
				"description": "Job definition for add/update",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "System event text for wake action",
			},
		},
		"required": []string{"action"},
	}
}

func toolCron() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			JobID  string `json:"jobId"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid cron params: %w", err)
		}

		switch p.Action {
		case "status", "list":
			return "No cron jobs configured.", nil
		case "add":
			return "Cron job scheduling is not yet implemented in the Go gateway. Use the Node.js gateway for cron support.", nil
		case "remove":
			return fmt.Sprintf("Cron job %q not found.", p.JobID), nil
		default:
			return fmt.Sprintf("Cron action %q acknowledged. Full cron support coming soon.", p.Action), nil
		}
	}
}

// --- gateway tool ---

func gatewayToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Gateway action: restart, config.get, config.schema.lookup, config.apply, config.patch, update.run",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Config path for schema.lookup",
			},
			"raw": map[string]any{
				"type":        "string",
				"description": "Raw config JSON for apply/patch",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Reason for restart",
			},
		},
		"required": []string{"action"},
	}
}

func toolGateway() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid gateway params: %w", err)
		}

		switch p.Action {
		case "config.get":
			// Read deneb.json and return.
			home, _ := os.UserHomeDir()
			data, err := os.ReadFile(home + "/.deneb/deneb.json")
			if err != nil {
				return "Failed to read config: " + err.Error(), nil
			}
			return string(data), nil
		case "config.schema.lookup":
			return fmt.Sprintf("Config schema lookup for path %q is not yet implemented.", p.Path), nil
		case "restart":
			return "Gateway restart requested. Use `deneb gateway restart` from the CLI.", nil
		case "config.apply", "config.patch":
			return "Config apply/patch is not yet implemented in the Go gateway. Edit ~/.deneb/deneb.json directly.", nil
		case "update.run":
			return "Self-update is not yet implemented in the Go gateway.", nil
		default:
			return fmt.Sprintf("Unknown gateway action: %q", p.Action), nil
		}
	}
}

// --- sessions_list tool ---

func sessionsListToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "number",
				"description": "Maximum sessions to return",
			},
			"kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Filter by session kind: main, group, cron, hook",
			},
		},
	}
}

func toolSessionsList() ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		sessionKey := SessionKeyFromContext(ctx)
		return fmt.Sprintf("Current session: %s\nNo other sessions available (Go gateway single-session mode).", sessionKey), nil
	}
}

// --- sessions_history tool ---

func sessionsHistoryToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Session key to fetch history for",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of messages to return",
			},
		},
		"required": []string{"sessionKey"},
	}
}

func toolSessionsHistory() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_history params: %w", err)
		}
		return fmt.Sprintf("Session history for %q is not accessible from tools. Use the sessions RPC API.", p.SessionKey), nil
	}
}

// --- sessions_send tool ---

func sessionsSendToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Target session key",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to send",
			},
		},
		"required": []string{"message"},
	}
}

func toolSessionsSend() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_send params: %w", err)
		}
		return fmt.Sprintf("Cross-session messaging is not yet implemented. Message for %q: %q", p.SessionKey, p.Message), nil
	}
}

// --- sessions_spawn tool ---

func sessionsSpawnToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Task description for the sub-agent",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Human-readable label",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model override for the sub-agent",
			},
		},
		"required": []string{"task"},
	}
}

func toolSessionsSpawn() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task string `json:"task"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_spawn params: %w", err)
		}
		return fmt.Sprintf("Sub-agent spawning is not yet implemented. Task: %q", p.Task), nil
	}
}

// --- subagents tool ---

func subagentsToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: list, kill, steer",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Target sub-agent ID or label",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Steering message for steer action",
			},
		},
	}
}

func toolSubagents() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid subagents params: %w", err)
		}
		if p.Action == "" {
			p.Action = "list"
		}

		switch p.Action {
		case "list":
			return "No active sub-agents.", nil
		case "kill":
			return "No sub-agent to kill.", nil
		case "steer":
			return "No sub-agent to steer.", nil
		default:
			return fmt.Sprintf("Unknown subagents action: %q", p.Action), nil
		}
	}
}

// --- session_status tool ---

func sessionStatusToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Session key (defaults to current)",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Set model override for the session",
			},
		},
	}
}

func toolSessionStatus() ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		sessionKey := SessionKeyFromContext(ctx)
		if sessionKey == "" {
			sessionKey = "(unknown)"
		}

		now := time.Now()
		return fmt.Sprintf("📊 Session Status\n"+
			"Session: %s\n"+
			"Time: %s\n"+
			"Gateway: Go\n"+
			"Status: running",
			sessionKey, now.Format("2006-01-02 15:04:05")), nil
	}
}

// --- image tool ---

func imageToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze in the image(s)",
			},
			"image": map[string]any{
				"type":        "string",
				"description": "Single image path or URL",
			},
			"images": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Multiple image paths or URLs (up to 20)",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Vision model to use",
			},
		},
	}
}

func toolImage() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Image  string   `json:"image"`
			Images []string `json:"images"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid image params: %w", err)
		}
		count := 0
		if p.Image != "" {
			count = 1
		}
		count += len(p.Images)
		if count == 0 {
			return "No images provided. Use 'image' for a single path/URL or 'images' for multiple.", nil
		}
		return fmt.Sprintf("Image analysis for %d image(s) is not yet implemented. Images are already visible to you when sent in the user's message.", count), nil
	}
}

// --- nodes tool ---

func nodesToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: status, describe, notify, camera_snap, location_get, run, invoke",
			},
			"node": map[string]any{
				"type":        "string",
				"description": "Node ID or name",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Notification title (for notify action)",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Notification body (for notify action)",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command to run (for run action)",
			},
		},
		"required": []string{"action"},
	}
}

func toolNodes() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid nodes params: %w", err)
		}

		switch p.Action {
		case "status":
			return "No paired nodes found.", nil
		case "describe":
			return "No nodes available to describe.", nil
		case "notify":
			return "Node notification requires a paired device. No nodes connected.", nil
		default:
			return fmt.Sprintf("Nodes action %q requires a paired device. No nodes connected.", p.Action), nil
		}
	}
}
