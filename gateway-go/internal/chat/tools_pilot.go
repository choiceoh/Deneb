package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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
	pilotBaseTimeout    = 2 * time.Minute
	pilotPerSourceExtra = 15 * time.Second // dynamic timeout: base + per-source
	pilotMaxInput       = 32000            // chars — auto-truncate beyond this (DGX Spark has ample memory)
	pilotMaxTokens      = 4096
	pilotMaxSources     = 15
)

func buildPilotSystemPrompt(workspaceDir string) string {
	base := `You are Pilot, a fast local AI assistant.
Rules:
- Execute the task directly. No preamble, no pleasantries.
- Match the user's language (Korean if Korean input, English if English).
- If output_format is "json", return valid JSON only.
- If output_format is "list", return a numbered list.
- If processing multiple sources, reference each by its label.
- Be concise. Substance over length.`

	if workspaceDir != "" {
		return base + "\n\nWorkspace directory: " + workspaceDir
	}
	return base
}

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
							"description": "Tool name from the registry (read, exec, grep, find, web_fetch, ls, http, kv, memory_search, etc.)",
						},
						"input": map[string]any{
							"type":        "object",
							"description": "Tool input parameters (same schema as calling the tool directly)",
						},
						"label": map[string]any{
							"type":        "string",
							"description": "Label for this source in the analysis (auto-generated if omitted)",
						},
						"only_if": map[string]any{
							"type":        "string",
							"description": "Only execute if the source with this label succeeded (non-empty, no error)",
						},
						"skip_if": map[string]any{
							"type":        "string",
							"description": "Skip this source if the source with this label succeeded",
						},
					},
					"required": []string{"tool", "input"},
				},
				"description": "Tool calls to execute before analysis. Pilot runs these, collects results, then processes everything with the local AI. Supports conditional execution via only_if/skip_if",
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
			"http": map[string]any{
				"type":        "string",
				"description": "Shortcut: GET this URL via http tool (expands to sources:[{tool:'http', input:{url:..., method:'GET'}}])",
			},
			"kv_key": map[string]any{
				"type":        "string",
				"description": "Shortcut: get this key from KV store (expands to sources:[{tool:'kv', input:{action:'get', key:...}}])",
			},
			"memory": map[string]any{
				"type":        "string",
				"description": "Shortcut: search memory for this query (expands to sources:[{tool:'memory_search', input:{query:...}}])",
			},
			"output_format": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "json", "list"},
				"description": "Desired output format (default: text)",
			},
			"post_process": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"description": "Post-processing action to apply to gathered source data before LLM analysis",
							"enum":        []string{"filter_lines", "head", "tail", "unique", "sort"},
						},
						"param": map[string]any{
							"type":        "string",
							"description": "Action parameter: regex for filter_lines, line count for head/tail",
						},
					},
					"required": []string{"action"},
				},
				"description": "Post-processing steps applied to gathered data before LLM analysis",
			},
		},
		"required": []string{"task"},
	}
}

// pilotTimeout computes a dynamic timeout based on source count.
func pilotTimeout(sourceCount int) time.Duration {
	return pilotBaseTimeout + time.Duration(sourceCount)*pilotPerSourceExtra
}

// toolPilot creates the pilot ToolFunc. It uses the ToolExecutor to run
// source tools from the registry before feeding results to the local LLM.
func toolPilot(tools ToolExecutor, workspaceDir string) ToolFunc {
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

		// Phase 1: Execute sources (unconditional in parallel, then conditional).
		gathered := executeSources(ctx, sources, tools)

		// Phase 1.5: Apply post-processing steps to gathered data.
		if len(p.PostProcess) > 0 {
			gathered = applyPostProcessSteps(gathered, p.PostProcess)
		}

		// Add direct content/items.
		if p.Content != "" {
			gathered = append(gathered, sourceResult{"content", p.Content})
		}
		for i, item := range p.Items {
			gathered = append(gathered, sourceResult{fmt.Sprintf("item[%d]", i+1), item})
		}

		// Phase 2: Build prompt and call local LLM with dynamic timeout.
		timeout := pilotTimeout(len(sources))
		llmCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		userMsg := buildPilotPrompt(p.Task, p.OutputFormat, gathered)
		result, err := callLocalLLM(llmCtx, buildPilotSystemPrompt(workspaceDir), userMsg, pilotMaxTokens)
		if err != nil {
			return "", fmt.Errorf("pilot: %w", err)
		}

		return result, nil
	}
}

// pilotParams is the parsed tool input.
type pilotParams struct {
	Task         string            `json:"task"`
	Sources      []sourceSpec      `json:"sources"`
	Content      string            `json:"content"`
	Items        []string          `json:"items"`
	OutputFormat string            `json:"output_format"`
	PostProcess  []postProcessStep `json:"post_process"`

	// Shortcuts.
	File   string   `json:"file"`
	Files  []string `json:"files"`
	Exec   string   `json:"exec"`
	Grep   string   `json:"grep"`
	Path   string   `json:"path"`
	URL    string   `json:"url"`
	HTTP   string   `json:"http"`
	KVKey  string   `json:"kv_key"`
	Memory string   `json:"memory"`
}

// postProcessStep is a programmatic transformation applied to gathered data.
type postProcessStep struct {
	Action string `json:"action"` // filter_lines, head, tail, unique, sort
	Param  string `json:"param"`  // action-specific parameter
}

// sourceSpec is a tool call specification from the agent.
type sourceSpec struct {
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
	Label  string          `json:"label"`
	OnlyIf string          `json:"only_if"` // execute only if named source succeeded
	SkipIf string          `json:"skip_if"` // skip if named source succeeded
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

	if p.HTTP != "" {
		specs = append(specs, sourceSpec{
			Tool:  "http",
			Input: mustJSON(map[string]any{"url": p.HTTP, "method": "GET"}),
			Label: "http: " + p.HTTP,
		})
	}

	if p.KVKey != "" {
		specs = append(specs, sourceSpec{
			Tool:  "kv",
			Input: mustJSON(map[string]any{"action": "get", "key": p.KVKey}),
			Label: "kv: " + p.KVKey,
		})
	}

	if p.Memory != "" {
		specs = append(specs, sourceSpec{
			Tool:  "memory_search",
			Input: mustJSON(map[string]any{"query": p.Memory}),
			Label: "memory: " + p.Memory,
		})
	}

	return specs
}

// executeSources runs source tool calls via the ToolRegistry.
// Unconditional sources (no only_if/skip_if) run in parallel first.
// Conditional sources run sequentially after, evaluating their conditions.
func executeSources(ctx context.Context, sources []sourceSpec, tools ToolExecutor) []sourceResult {
	if len(sources) == 0 {
		return nil
	}

	results := make([]sourceResult, len(sources))

	// Split into unconditional and conditional.
	type indexedSource struct {
		idx int
		src sourceSpec
	}
	var unconditional, conditional []indexedSource
	for i, src := range sources {
		label := src.Label
		if label == "" {
			label = fmt.Sprintf("%s[%d]", src.Tool, i+1)
			sources[i].Label = label
		}

		if src.OnlyIf != "" || src.SkipIf != "" {
			conditional = append(conditional, indexedSource{i, src})
		} else {
			unconditional = append(unconditional, indexedSource{i, src})
		}
	}

	// Phase 1: Run unconditional sources in parallel.
	var wg sync.WaitGroup
	for _, is := range unconditional {
		if is.src.Tool == "pilot" {
			results[is.idx] = sourceResult{
				label:   is.src.Label,
				content: "[error: pilot cannot call itself]",
			}
			continue
		}
		wg.Add(1)
		go func(idx int, s sourceSpec) {
			defer wg.Done()
			output, err := tools.Execute(ctx, s.Tool, s.Input)
			if err != nil {
				results[idx] = sourceResult{s.Label, fmt.Sprintf("[tool error: %s]", err)}
				return
			}
			results[idx] = sourceResult{s.Label, output}
		}(is.idx, is.src)
	}
	wg.Wait()

	// Phase 2: Run conditional sources sequentially.
	for _, is := range conditional {
		src := is.src

		if src.Tool == "pilot" {
			results[is.idx] = sourceResult{
				label:   src.Label,
				content: "[error: pilot cannot call itself]",
			}
			continue
		}

		// Evaluate condition.
		if src.OnlyIf != "" && !sourceSucceeded(results, src.OnlyIf) {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[skipped: %q did not succeed]", src.OnlyIf)}
			continue
		}
		if src.SkipIf != "" && sourceSucceeded(results, src.SkipIf) {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[skipped: %q succeeded]", src.SkipIf)}
			continue
		}

		output, err := tools.Execute(ctx, src.Tool, src.Input)
		if err != nil {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[tool error: %s]", err)}
			continue
		}
		results[is.idx] = sourceResult{src.Label, output}
	}

	return results
}

// sourceSucceeded checks if a source with the given label has a non-empty, non-error result.
func sourceSucceeded(results []sourceResult, label string) bool {
	for _, r := range results {
		if r.label == label {
			return r.content != "" && !strings.HasPrefix(r.content, "[tool error:") && !strings.HasPrefix(r.content, "[skipped:")
		}
	}
	return false
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

// --- Post-process steps ---

// applyPostProcessSteps applies programmatic transformations to gathered data
// before feeding it to the LLM. This reduces noise without burning LLM tokens.
func applyPostProcessSteps(gathered []sourceResult, steps []postProcessStep) []sourceResult {
	for _, step := range steps {
		for i := range gathered {
			gathered[i].content = applyStep(gathered[i].content, step)
		}
	}
	return gathered
}

func applyStep(content string, step postProcessStep) string {
	lines := strings.Split(content, "\n")

	switch step.Action {
	case "filter_lines":
		if step.Param == "" {
			return content
		}
		re, err := regexp.Compile(step.Param)
		if err != nil {
			return content
		}
		var filtered []string
		for _, line := range lines {
			if re.MatchString(line) {
				filtered = append(filtered, line)
			}
		}
		return strings.Join(filtered, "\n")

	case "head":
		n := parseLineCount(step.Param, 20)
		if n >= len(lines) {
			return content
		}
		return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n[... %d more lines]", len(lines)-n)

	case "tail":
		n := parseLineCount(step.Param, 20)
		if n >= len(lines) {
			return content
		}
		return fmt.Sprintf("[%d lines before ...]\n", len(lines)-n) + strings.Join(lines[len(lines)-n:], "\n")

	case "unique":
		seen := make(map[string]bool)
		var unique []string
		for _, line := range lines {
			if !seen[line] {
				seen[line] = true
				unique = append(unique, line)
			}
		}
		return strings.Join(unique, "\n")

	case "sort":
		sorted := make([]string, len(lines))
		copy(sorted, lines)
		sort.Strings(sorted)
		return strings.Join(sorted, "\n")

	default:
		return content
	}
}

func parseLineCount(s string, defaultN int) int {
	if s == "" {
		return defaultN
	}
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n <= 0 {
		return defaultN
	}
	return n
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
