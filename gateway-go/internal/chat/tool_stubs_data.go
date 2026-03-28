package chat

import (
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

func toolSubagents(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action  string `json:"action"`
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := jsonutil.UnmarshalInto("subagents params", input, &p); err != nil {
			return "", err
		}
		if p.Action == "" {
			p.Action = "list"
		}

		if deps == nil || deps.Sessions == nil {
			return "Sub-agent management not available (session dependencies not wired).", nil
		}

		parentKey := SessionKeyFromContext(ctx)

		// Gather children: sessions where SpawnedBy == parentKey.
		allSessions := deps.Sessions.List()
		var children []*session.Session
		for _, s := range allSessions {
			if s.SpawnedBy == parentKey {
				children = append(children, s)
			}
		}

		// Sort: running first, then by UpdatedAt descending.
		sort.Slice(children, func(i, j int) bool {
			iRunning := children[i].Status == session.StatusRunning
			jRunning := children[j].Status == session.StatusRunning
			if iRunning != jRunning {
				return iRunning
			}
			return children[i].UpdatedAt > children[j].UpdatedAt
		})

		switch p.Action {
		case "list":
			return subagentsList(children), nil
		case "kill":
			return subagentsKill(deps, children, p.Target)
		case "steer":
			return subagentsSteer(deps, children, p.Target, p.Message)
		default:
			return fmt.Sprintf("Unknown subagents action: %q", p.Action), nil
		}
	}
}

// subagentsList formats the children list for display.
func subagentsList(children []*session.Session) string {
	if len(children) == 0 {
		return "No sub-agents."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Sub-agents (%d):\n", len(children))
	for i, c := range children {
		label := c.Label
		if label == "" {
			label = c.Key
		}
		status := string(c.Status)
		if status == "" {
			status = "unknown"
		}

		var parts []string
		// Runtime.
		if c.RuntimeMs != nil {
			parts = append(parts, autoreply.FormatDurationCompact(*c.RuntimeMs))
		} else if c.Status == session.StatusRunning && c.StartedAt != nil {
			elapsed := time.Now().UnixMilli() - *c.StartedAt
			parts = append(parts, autoreply.FormatDurationCompact(elapsed))
		}
		// Tokens.
		if c.TotalTokens != nil && *c.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("%dtok", *c.TotalTokens))
		}
		// Model.
		if c.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", c.Model))
		}

		fmt.Fprintf(&sb, "  %d. [%s] %s", i+1, status, autoreply.TruncateLine(label, 60))
		if len(parts) > 0 {
			fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// subagentsKill kills one or all child sessions.
func subagentsKill(deps *CoreToolDeps, children []*session.Session, target string) (string, error) {
	if target == "" {
		return "Target is required. Use a sub-agent index, label, session key, or \"all\".", nil
	}

	if strings.ToLower(target) == "all" {
		killed := 0
		for _, c := range children {
			if c.Status == session.StatusRunning {
				killSession(deps.Sessions, c)
				killed++
			}
		}
		if killed == 0 {
			return "No running sub-agents to kill.", nil
		}
		return fmt.Sprintf("Killed %d sub-agent(s).", killed), nil
	}

	child, errMsg := resolveChildTarget(children, target)
	if errMsg != "" {
		return errMsg, nil
	}
	if child.Status != session.StatusRunning {
		return fmt.Sprintf("Sub-agent %q is not running (status: %s).", child.Key, child.Status), nil
	}
	killSession(deps.Sessions, child)
	return fmt.Sprintf("Killed sub-agent: %s", child.Key), nil
}

// subagentsSteer sends a steering message to a running child session.
func subagentsSteer(deps *CoreToolDeps, children []*session.Session, target, message string) (string, error) {
	if deps.SessionSendFn == nil {
		return "Steering not available (SessionSendFn not wired).", nil
	}
	if message == "" {
		return "Message is required for steer action.", nil
	}

	// Auto-target if exactly one running child and no target specified.
	if target == "" {
		var running []*session.Session
		for _, c := range children {
			if c.Status == session.StatusRunning {
				running = append(running, c)
			}
		}
		switch len(running) {
		case 0:
			return "No running sub-agents to steer.", nil
		case 1:
			target = running[0].Key
		default:
			return "Multiple running sub-agents. Specify a target (index, label, or key).", nil
		}
	}

	child, errMsg := resolveChildTarget(children, target)
	if errMsg != "" {
		return errMsg, nil
	}
	if child.Status != session.StatusRunning {
		return fmt.Sprintf("Sub-agent %q is not running (status: %s).", child.Key, child.Status), nil
	}

	if err := deps.SessionSendFn(child.Key, message); err != nil {
		return fmt.Sprintf("Failed to steer sub-agent %q: %s", child.Key, err.Error()), nil
	}
	return fmt.Sprintf("Steered sub-agent: %s\nMessage: %s", child.Key, message), nil
}

// killSession applies the kill pattern (mirrors http_session_kill.go).
func killSession(sessions *session.Manager, s *session.Session) {
	now := time.Now().UnixMilli()
	s.Status = session.StatusKilled
	s.EndedAt = &now
	if s.StartedAt != nil {
		runtime := now - *s.StartedAt
		s.RuntimeMs = &runtime
	}
	s.UpdatedAt = now
	_ = sessions.Set(s) // RUNNING → KILLED is always valid; error unreachable
}

// resolveChildTarget finds a child by 1-based index, exact key, label, or key prefix.
func resolveChildTarget(children []*session.Session, target string) (*session.Session, string) {
	if target == "" {
		return nil, "Missing sub-agent target."
	}

	// Try numeric index (1-based).
	if len(target) <= 3 {
		idx := 0
		isNum := true
		for _, c := range target {
			if c < '0' || c > '9' {
				isNum = false
				break
			}
			idx = idx*10 + int(c-'0')
		}
		if isNum && idx >= 1 && idx <= len(children) {
			return children[idx-1], ""
		}
		if isNum {
			return nil, fmt.Sprintf("Invalid sub-agent index: %s (have %d sub-agents)", target, len(children))
		}
	}

	// Try exact session key.
	for _, c := range children {
		if c.Key == target {
			return c, ""
		}
	}

	// Try label match (case-insensitive exact, then prefix).
	lowered := strings.ToLower(target)
	var exactLabel []*session.Session
	var prefixLabel []*session.Session
	for _, c := range children {
		l := strings.ToLower(c.Label)
		if l == lowered {
			exactLabel = append(exactLabel, c)
		} else if strings.HasPrefix(l, lowered) {
			prefixLabel = append(prefixLabel, c)
		}
	}
	if len(exactLabel) == 1 {
		return exactLabel[0], ""
	}
	if len(exactLabel) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent label: %s", target)
	}
	if len(prefixLabel) == 1 {
		return prefixLabel[0], ""
	}
	if len(prefixLabel) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent label prefix: %s", target)
	}

	// Try session key prefix.
	var keyPrefix []*session.Session
	for _, c := range children {
		if strings.HasPrefix(c.Key, target) {
			keyPrefix = append(keyPrefix, c)
		}
	}
	if len(keyPrefix) == 1 {
		return keyPrefix[0], ""
	}
	if len(keyPrefix) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent key prefix: %s", target)
	}

	return nil, fmt.Sprintf("Unknown sub-agent: %s", target)
}

// --- session_status tool ---

// --- image tool ---

func toolImage(client *llm.Client) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Prompt string   `json:"prompt"`
			Image  string   `json:"image"`
			Images []string `json:"images"`
			Model  string   `json:"model"`
		}
		if err := jsonutil.UnmarshalInto("image params", input, &p); err != nil {
			return "", err
		}

		// Collect all image paths/URLs.
		var imagePaths []string
		if p.Image != "" {
			imagePaths = append(imagePaths, p.Image)
		}
		imagePaths = append(imagePaths, p.Images...)
		if len(imagePaths) == 0 {
			return "No images provided. Use 'image' for a single path/URL or 'images' for multiple.", nil
		}
		if len(imagePaths) > 20 {
			return "Too many images (max 20).", nil
		}

		if client == nil {
			return fmt.Sprintf("Vision model not available. %d image(s) provided but no LLM client configured. Images sent in the user's message are already visible to you.", len(imagePaths)), nil
		}

		prompt := p.Prompt
		if prompt == "" {
			prompt = "Describe what you see in the image(s) in detail."
		}

		// Build content blocks with images + text prompt.
		var blocks []llm.ContentBlock
		for _, imgPath := range imagePaths {
			block, err := loadImageBlock(ctx, imgPath)
			if err != nil {
				return fmt.Sprintf("Failed to load image %q: %s", imgPath, err.Error()), nil
			}
			blocks = append(blocks, block)
		}
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: prompt})

		model := p.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}

		// Call LLM with vision.
		visionCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		events, err := client.StreamChat(visionCtx, llm.ChatRequest{
			Model:     model,
			Messages:  []llm.Message{llm.NewBlockMessage("user", blocks)},
			MaxTokens: 4096,
			Stream:    true,
		})
		if err != nil {
			return fmt.Sprintf("Vision model call failed: %s", err.Error()), nil
		}

		// Collect streaming text response.
		var result strings.Builder
		for ev := range events {
			if ev.Type == "content_block_delta" {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					result.WriteString(delta.Delta.Text)
				}
			}
		}

		if result.Len() == 0 {
			return "Vision model returned no response.", nil
		}
		return result.String(), nil
	}
}

// loadImageBlock loads an image from a file path or URL and returns an LLM content block.
func loadImageBlock(ctx context.Context, path string) (llm.ContentBlock, error) {
	var data []byte
	var mimeType string

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// URL: use OpenAI-style image_url block.
		return llm.ContentBlock{
			Type:     "image",
			ImageURL: &llm.ImageURL{URL: path, Detail: "auto"},
		}, nil
	}

	// Local file: read and base64-encode.
	var err error
	data, err = os.ReadFile(path)
	if err != nil {
		return llm.ContentBlock{}, fmt.Errorf("read image file: %w", err)
	}

	// Detect MIME type from magic bytes.
	mimeType = http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png" // fallback
	}

	encoded := base64Encode(data)
	return llm.ContentBlock{
		Type: "image",
		Source: &llm.ImageSource{
			Type:      "base64",
			MediaType: mimeType,
			Data:      encoded,
		},
	}, nil
}

// base64Encode encodes data to standard base64.
func base64Encode(data []byte) string {
	return b64.StdEncoding.EncodeToString(data)
}
