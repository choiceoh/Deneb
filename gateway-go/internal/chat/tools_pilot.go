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

// pilotInput is the parsed input for the pilot tool.
type pilotInput struct {
	Mode    string `json:"mode"`
	Content string `json:"content"`
	Prompt  string `json:"prompt"`
}

// pilotMode defines a supported pilot task mode with its system prompt.
type pilotMode struct {
	system string
	// maxTokens overrides the default if set.
	maxTokens int
}

// pilotModes maps mode names to their system prompts and configs.
var pilotModes = map[string]pilotMode{
	"summarize": {
		system:    "You are a concise summarizer. Summarize the given text, focusing on key points only. Reply in the same language as the input. Be brief and direct.",
		maxTokens: 2048,
	},
	"analyze_code": {
		system:    "You are a code reviewer. Analyze the given code and report: (1) bugs or potential issues, (2) security concerns, (3) improvement suggestions. Be specific with line references. Reply in Korean.",
		maxTokens: 4096,
	},
	"classify": {
		system:    "You are a text classifier. Classify the given text by category, intent, and sentiment. Return a structured brief response. Reply in Korean.",
		maxTokens: 1024,
	},
	"ask": {
		system:    "You are a helpful assistant. Answer the question based on the provided context. Be accurate and concise. Reply in Korean unless the context is in another language.",
		maxTokens: 4096,
	},
	"translate": {
		system:    "You are a translator. Translate the given text to Korean. If already Korean, translate to English. Preserve formatting and technical terms.",
		maxTokens: 4096,
	},
}

const (
	pilotTimeout        = 2 * time.Minute
	pilotDefaultMaxToks = 2048
)

func pilotToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"summarize", "analyze_code", "classify", "ask", "translate"},
				"description": "Task type: summarize, analyze_code, classify, ask, translate",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text or code to process",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Additional instructions (optional; for ask mode, this is the question)",
			},
		},
		"required": []string{"mode", "content"},
	}
}

// toolPilot returns a ToolFunc that delegates lightweight tasks to the local
// sglang model (Qwen). This saves external API costs for simple operations
// like summarization, code review, classification, and translation.
func toolPilot() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p pilotInput
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid pilot input: %w", err)
		}
		if p.Content == "" {
			return "", fmt.Errorf("content is required")
		}

		mode, ok := pilotModes[p.Mode]
		if !ok {
			modes := make([]string, 0, len(pilotModes))
			for k := range pilotModes {
				modes = append(modes, k)
			}
			return "", fmt.Errorf("unknown mode %q; supported: %s", p.Mode, strings.Join(modes, ", "))
		}

		// Build user message. For "ask" mode, content is context and prompt is the question.
		var userText string
		if p.Mode == "ask" && p.Prompt != "" {
			userText = fmt.Sprintf("Context:\n%s\n\nQuestion: %s", p.Content, p.Prompt)
		} else if p.Prompt != "" {
			userText = fmt.Sprintf("%s\n\nAdditional instructions: %s", p.Content, p.Prompt)
		} else {
			userText = p.Content
		}

		maxTokens := mode.maxTokens
		if maxTokens <= 0 {
			maxTokens = pilotDefaultMaxToks
		}

		// Call local sglang.
		result, err := callLocalLLM(ctx, mode.system, userText, maxTokens)
		if err != nil {
			return "", fmt.Errorf("pilot (%s): %w", p.Mode, err)
		}

		return result, nil
	}
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

	// Collect full response from stream.
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
// the concatenated text output. This is a simplified version of consumeStream
// for non-interactive tool use.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			// Return whatever we collected so far.
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
