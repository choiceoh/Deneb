package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Pilot tool: the AI agent's fast local helper that can orchestrate other tools.
//
// The agent specifies a task and data sources. Pilot:
//   1. Executes source tool calls via the ToolRegistry (parallel)
//   2. Feeds all gathered data + task to the local sglang model
//   3. Returns the result synchronously
//
// This turns multi-call workflows into a single pilot call:
//   Before: read("main.go") → [wait] → pilot(task, content=result)  (2 turns)
//   After:  pilot(task, sources=[{tool:"read", input:{file_path:"main.go"}}])  (1 turn)
//
// Shortcuts (file, exec, grep, url) expand to sources internally for convenience.

const (
	pilotTimeout     = 2 * time.Minute
	pilotMaxInput    = 24000 // chars — auto-truncate beyond this
	pilotMaxTokens   = 4096
	pilotMaxSources  = 10
)

const pilotSystemPrompt = `You are Pilot, a fast local AI assistant.
Rules:
- Execute the task directly. No preamble, no pleasantries.
- Match the user's language (Korean if Korean input, English if English).
- If output_format is "json", return valid JSON only.
- If output_format is "list", return a numbered list.
- If processing multiple sources, reference each by its label.
- Be concise. Substance over length.`

func pilotToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "What to do — free-form instruction",
			},
			"sources": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool": map[string]any{
							"type":        "string",
							"description": "Tool name from the registry (read, exec, grep, find, web_fetch, ls, http, etc.)",
						},
						"input": map[string]any{
							"type":        "object",
							"description": "Tool input parameters (same schema as calling the tool directly)",
						},
						"label": map[string]any{
							"type":        "string",
							"description": "Label for this source in the analysis (auto-generated if omitted)",
						},
					},
					"required": []string{"tool", "input"},
				},
				"description": "Tool calls to execute before analysis. Pilot runs these, collects results, then processes everything with the local AI",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Direct text/code input (no tool call needed)",
			},
			"items": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Multiple items to process in batch",
			},
			// Shortcuts — expand to sources internally.
			"file": map[string]any{
				"type":        "string",
				"description": "Shortcut: read this file (expands to sources:[{tool:'read', input:{file_path:...}}])",
			},
			"files": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Shortcut: read multiple files",
			},
			"exec": map[string]any{
				"type":        "string",
				"description": "Shortcut: run this command (expands to sources:[{tool:'exec', input:{command:...}}])",
			},
			"grep": map[string]any{
				"type":        "string",
				"description": "Shortcut: grep for this pattern (use with 'path')",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path for grep shortcut",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Shortcut: fetch this URL (expands to sources:[{tool:'web_fetch', input:{url:...}}])",
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

// toolPilot creates the pilot ToolFunc. It uses the ToolExecutor to run
// source tools from the registry before feeding results to the local LLM.
func toolPilot(tools ToolExecutor) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p pilotParams
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid pilot input: %w", err)
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		// Expand shortcuts into source specs.
		sources := expandShortcuts(p)

		// Merge with explicit sources.
		sources = append(sources, p.Sources...)

		// Cap source count.
		if len(sources) > pilotMaxSources {
			sources = sources[:pilotMaxSources]
		}

		// Phase 1: Execute all source tools in parallel.
		gathered := executeSources(ctx, sources, tools)

		// Add direct content/items.
		if p.Content != "" {
			gathered = append(gathered, sourceResult{"content", p.Content})
		}
		for i, item := range p.Items {
			gathered = append(gathered, sourceResult{fmt.Sprintf("item[%d]", i+1), item})
		}

		// Phase 2: Build prompt and call local LLM.
		userMsg := buildPilotPrompt(p.Task, p.OutputFormat, gathered)
		result, err := callLocalLLM(ctx, pilotSystemPrompt, userMsg, pilotMaxTokens)
		if err != nil {
			return "", fmt.Errorf("pilot: %w", err)
		}

		return result, nil
	}
}

// pilotParams is the parsed tool input.
type pilotParams struct {
	Task         string       `json:"task"`
	Sources      []sourceSpec `json:"sources"`
	Content      string       `json:"content"`
	Items        []string     `json:"items"`
	OutputFormat string       `json:"output_format"`

	// Shortcuts.
	File  string   `json:"file"`
	Files []string `json:"files"`
	Exec  string   `json:"exec"`
	Grep  string   `json:"grep"`
	Path  string   `json:"path"`
	URL   string   `json:"url"`
}

// sourceSpec is a tool call specification from the agent.
type sourceSpec struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	Label string          `json:"label"`
}

// sourceResult is a labeled chunk of gathered data.
type sourceResult struct {
	label   string
	content string
}

// expandShortcuts converts convenience params (file, exec, grep, url) into sourceSpecs.
func expandShortcuts(p pilotParams) []sourceSpec {
	var specs []sourceSpec

	if p.File != "" {
		specs = append(specs, sourceSpec{
			Tool:  "read",
			Input: mustJSON(map[string]any{"file_path": p.File}),
			Label: p.File,
		})
	}

	for _, f := range p.Files {
		specs = append(specs, sourceSpec{
			Tool:  "read",
			Input: mustJSON(map[string]any{"file_path": f}),
			Label: f,
		})
	}

	if p.Exec != "" {
		specs = append(specs, sourceSpec{
			Tool:  "exec",
			Input: mustJSON(map[string]any{"command": p.Exec, "timeout": 15}),
			Label: "$ " + p.Exec,
		})
	}

	if p.Grep != "" {
		grepInput := map[string]any{"pattern": p.Grep, "maxResults": 50}
		if p.Path != "" {
			grepInput["path"] = p.Path
		}
		specs = append(specs, sourceSpec{
			Tool:  "grep",
			Input: mustJSON(grepInput),
			Label: "grep: " + p.Grep,
		})
	}

	if p.URL != "" {
		specs = append(specs, sourceSpec{
			Tool:  "web_fetch",
			Input: mustJSON(map[string]any{"url": p.URL}),
			Label: p.URL,
		})
	}

	return specs
}

// executeSources runs all source tool calls in parallel via the ToolRegistry.
func executeSources(ctx context.Context, sources []sourceSpec, tools ToolExecutor) []sourceResult {
	if len(sources) == 0 {
		return nil
	}

	results := make([]sourceResult, len(sources))
	var wg sync.WaitGroup

	for i, src := range sources {
		// Block pilot from calling itself (infinite recursion guard).
		if src.Tool == "pilot" {
			results[i] = sourceResult{
				label:   src.Label,
				content: "[error: pilot cannot call itself]",
			}
			continue
		}

		wg.Add(1)
		go func(idx int, s sourceSpec) {
			defer wg.Done()

			label := s.Label
			if label == "" {
				label = fmt.Sprintf("%s[%d]", s.Tool, idx+1)
			}

			output, err := tools.Execute(ctx, s.Tool, s.Input)
			if err != nil {
				results[idx] = sourceResult{label, fmt.Sprintf("[tool error: %s]", err)}
				return
			}
			results[idx] = sourceResult{label, output}
		}(i, src)
	}

	wg.Wait()
	return results
}

// buildPilotPrompt assembles the user message from task + gathered data.
func buildPilotPrompt(task, outputFormat string, blocks []sourceResult) string {
	var sb strings.Builder

	sb.WriteString("Task: ")
	sb.WriteString(task)

	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
	}

	if len(blocks) == 0 {
		return sb.String()
	}

	// Budget per block to stay within total limit.
	perBlock := pilotMaxInput
	if len(blocks) > 1 {
		perBlock = pilotMaxInput / len(blocks)
		if perBlock < 2000 {
			perBlock = 2000
		}
	}

	for _, b := range blocks {
		sb.WriteString("\n\n--- ")
		sb.WriteString(b.label)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateInput(b.content, perBlock))
	}

	return sb.String()
}

// --- Helpers ---

func truncateInput(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// --- Local LLM call ---

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
