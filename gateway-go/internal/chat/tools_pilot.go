package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Pilot tool: the main AI agent's fast local helper.
//
// Why pilot instead of sessions_spawn (subagent)?
//   - Synchronous: 1 tool call → instant result (subagent needs spawn + poll + history = 3+ calls)
//   - Zero overhead: no session, transcript, lifecycle, or broadcasting
//   - Free-form: just describe the task, no mode selection needed
//   - Batch: process multiple items in a single call
//   - Local: runs on DGX Spark sglang, no external API cost

const (
	pilotTimeout   = 2 * time.Minute
	pilotMaxInput  = 24000 // chars — beyond this, auto-truncate with notice
	pilotMaxTokens = 4096
)

// pilotSystemPrompt is the baseline identity for all pilot calls.
const pilotSystemPrompt = `You are Pilot, a fast local AI assistant.
Rules:
- Execute the task directly. No preamble, no pleasantries.
- Match the user's language (Korean if Korean input, English if English).
- If output_format is "json", return valid JSON only.
- If output_format is "list", return a numbered list.
- If processing multiple items, handle each one and label results clearly.
- Be concise. Prefer substance over length.`

func pilotToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "What to do — free-form instruction (e.g., '이 코드 버그 찾아줘', 'summarize this', '한국어로 번역')",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text, code, or data to process",
			},
			"items": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Multiple items to process in batch (alternative to content for batch jobs)",
			},
			"output_format": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "json", "list"},
				"description": "Desired output format (default: text)",
			},
		},
		"required": []string{"task"},
	}
}

// toolPilot returns a ToolFunc that delegates tasks to the local sglang model.
// Unlike sessions_spawn, pilot is synchronous (1 call = 1 result) with zero
// session overhead. The agent just describes the task and gets the answer back.
func toolPilot() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task         string   `json:"task"`
			Content      string   `json:"content"`
			Items        []string `json:"items"`
			OutputFormat string   `json:"output_format"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid pilot input: %w", err)
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		// Build the user message from task + content/items.
		userMsg := buildPilotMessage(p.Task, p.Content, p.Items, p.OutputFormat)

		// Call local sglang.
		result, err := callLocalLLM(ctx, pilotSystemPrompt, userMsg, pilotMaxTokens)
		if err != nil {
			return "", fmt.Errorf("pilot: %w", err)
		}

		return result, nil
	}
}

// buildPilotMessage assembles the user prompt from task, content/items, and format.
func buildPilotMessage(task, content string, items []string, outputFormat string) string {
	var sb strings.Builder

	// Task instruction.
	sb.WriteString("Task: ")
	sb.WriteString(task)

	// Output format hint.
	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
	}

	// Content (single item) or items (batch).
	if len(items) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n--- %d items ---\n", len(items)))
		for i, item := range items {
			sb.WriteString(fmt.Sprintf("\n[%d]\n%s\n", i+1, truncateInput(item, pilotMaxInput/len(items))))
		}
	} else if content != "" {
		sb.WriteString("\n\n---\n")
		sb.WriteString(truncateInput(content, pilotMaxInput))
	}

	return sb.String()
}

// truncateInput shortens input to maxChars with a notice.
func truncateInput(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "\n\n[... truncated, showing first " + fmt.Sprintf("%d", maxChars) + " chars]"
}

// callLocalLLM sends a single-turn request to the local sglang server and
// collects the full response. Uses streaming internally but returns the
// complete text.
func callLocalLLM(ctx context.Context, system, userMessage string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := llm.NewClient(defaultSglangBaseURL, "", llm.WithLogger(slog.Default()))

	req := llm.ChatRequest{
		Model:     sglangModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
	}

	events, err := client.StreamChatOpenAI(ctx, req)
	if err != nil {
		return "", fmt.Errorf("sglang stream: %w", err)
	}

	text, err := collectStream(ctx, events)
	if err != nil {
		return "", err
	}

	if text == "" {
		return "(no response from local model)", nil
	}
	return text, nil
}

// collectStream reads all events from a streaming LLM response and returns
// the concatenated text output.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return sb.String(), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return sb.String(), nil
			}

			switch ev.Type {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			case "error":
				var errPayload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal(ev.Payload, &errPayload) == nil && errPayload.Error.Message != "" {
					return sb.String(), fmt.Errorf("stream error: %s", errPayload.Error.Message)
				}
			}
		}
	}
}
