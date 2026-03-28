package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)
// Pilot tool: the AI agent's fast local helper that can orchestrate other tools.
//
// The agent specifies a task and data sources. Pilot:
//  1. Checks sglang health (cached, 30s TTL)
//  2. Executes source tool calls via the ToolRegistry (parallel, per-source timeout)
//  3. Feeds all gathered data + task to the local sglang model
//  4. Optionally chains: if chain=true, LLM can request follow-up tool calls
//  5. Returns the result synchronously
//
// Shortcuts (file, exec, grep, find, url) expand to sources internally for convenience.

const (
	pilotTimeout     = 2 * time.Minute
	pilotMaxInput    = 24000 // chars — auto-truncate beyond this
	pilotMaxTokens   = 4096
	pilotMaxSources  = 10
	sourceTimeout    = 30 * time.Second // per-source tool execution timeout
	sglangHealthTTL  = 30 * time.Second
	sglangHealthPing = 3 * time.Second // HTTP timeout for health check
)

// --- sglang health check (cached) ---

var (
	sglangHealthy   atomic.Bool
	sglangLastCheck atomic.Int64 // unix timestamp
)

// checkSglangHealth returns true if the local sglang server is reachable.
// Result is cached for sglangHealthTTL to avoid per-call overhead.
func checkSglangHealth() bool {
	now := time.Now().Unix()
	last := sglangLastCheck.Load()
	if now-last < int64(sglangHealthTTL.Seconds()) {
		return sglangHealthy.Load()
	}

	// Probe /v1/models — lightweight endpoint.
	ctx, cancel := context.WithTimeout(context.Background(), sglangHealthPing)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultSglangBaseURL+"/models", nil)
	if err != nil {
		sglangHealthy.Store(false)
		sglangLastCheck.Store(now)
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sglangHealthy.Store(false)
		sglangLastCheck.Store(now)
		return false
	}
	resp.Body.Close()

	healthy := resp.StatusCode == http.StatusOK
	sglangHealthy.Store(healthy)
	sglangLastCheck.Store(now)
	return healthy
}

// --- Thinking mode for Qwen3.5 ---

// thinkingKeywords triggers thinking mode when the task contains complex analysis keywords.
var thinkingKeywords = []string{
	"분석", "비교", "리뷰", "디버그", "문제", "원인", "검토",
	"analyze", "compare", "review", "debug", "diagnose", "investigate", "diff",
}

// shouldUseThinking decides whether to enable Qwen3.5 thinking mode based on
// task complexity (keywords) and number of sources.
func shouldUseThinking(task string, sourceCount int) bool {
	if sourceCount >= 3 {
		return true
	}
	lower := strings.ToLower(task)
	for _, kw := range thinkingKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}


// --- System prompt ---

func buildPilotSystemPrompt(workspaceDir string, thinking bool) string {
	var sb strings.Builder
	sb.WriteString(`You are Pilot, a fast local AI assistant. Your output goes to Telegram (4096 char limit).
Rules:
- Execute the task directly. No preamble, no pleasantries.
- Match the user's language (Korean if Korean input, English if English).
- If output_format is "json", return valid JSON only (no markdown fences).
- If output_format is "list", return a clean numbered list (1. 2. 3.).
- If processing multiple sources, reference each by its label.
- When referencing code, include file path and line numbers.
- Use fenced code blocks with language tags for code snippets.
- Always close opened code blocks (matching triple backticks).
- Avoid nested markdown formatting inside code blocks.
- Be concise. Substance over length.`)

	if thinking {
		sb.WriteString("\n\nYou may use <think>...</think> for internal reasoning before your answer.")
	}

	if workspaceDir != "" {
		sb.WriteString("\n\nWorkspace directory: ")
		sb.WriteString(workspaceDir)
	}

	return sb.String()
}

// --- Tool schema ---

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
				"description": "Tool calls to execute before analysis. Supports conditional execution via only_if/skip_if",
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
			"find": map[string]any{
				"type":        "string",
				"description": "Shortcut: find files matching this pattern (use with 'path')",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path for grep/find shortcut",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Shortcut: fetch this URL (expands to sources:[{tool:'web_fetch', input:{url:...}}])",
			},
			"http": map[string]any{
				"type":        "string",
				"description": "Shortcut: GET this URL via http tool (expands to sources:[{tool:'http', input:{url:..., method:'GET'}}])",
			},
			"diff": map[string]any{
				"type":        "string",
				"description": "Shortcut: review code changes. Values: 'all' (uncommitted), 'staged', 'unstaged', or a commit hash (expands to sources:[{tool:'diff', input:{action:...}}])",
			},
			"test": map[string]any{
				"type":        "string",
				"description": "Shortcut: run tests and analyze results. Value: package path (e.g. 'gateway-go/...', './internal/chat/...') or 'all' (expands to sources:[{tool:'test', input:{action:'run', path:...}}])",
			},
			"tree": map[string]any{
				"type":        "string",
				"description": "Shortcut: show directory tree structure. Value: directory path (expands to sources:[{tool:'tree', input:{path:..., depth:3}}])",
			},
			"git_log": map[string]any{
				"type":        "string",
				"description": "Shortcut: show recent commit history. Values: 'recent' (last 20), a number (e.g. '10'), or 'oneline' (compact format) (expands to sources:[{tool:'git', input:{action:'log', ...}}])",
			},
			"health": map[string]any{
				"type":        "boolean",
				"description": "Shortcut: run infrastructure health check (expands to sources:[{tool:'health_check', input:{}}])",
			},
			"kv_key": map[string]any{
				"type":        "string",
				"description": "Shortcut: get this key from KV store (expands to sources:[{tool:'kv', input:{action:'get', key:...}}])",
			},
			"memory": map[string]any{
				"type":        "string",
				"description": "Shortcut: search memory for this query (expands to sources:[{tool:'memory_search', input:{query:...}}])",
			},
			"gmail": map[string]any{
				"type":        "string",
				"description": "Shortcut: search Gmail for this query (expands to sources:[{tool:'gmail', input:{action:'search', query:...}}])",
			},
			"youtube": map[string]any{
				"type":        "string",
				"description": "Shortcut: get YouTube transcript (expands to sources:[{tool:'youtube_transcript', input:{url:...}}])",
			},
			"polaris": map[string]any{
				"type":        "string",
				"description": "Shortcut: search Deneb system manual (expands to sources:[{tool:'polaris', input:{action:'search', query:...}}])",
			},
			"image": map[string]any{
				"type":        "string",
				"description": "Shortcut: analyze image file or URL (expands to sources:[{tool:'image', input:{paths:[...]}}])",
			},
			"vega": map[string]any{
				"type":        "string",
				"description": "Shortcut: search project knowledge base (expands to sources:[{tool:'vega', input:{query:...}}])",
			},
			"agent_logs": map[string]any{
				"type":        "string",
				"description": "Shortcut: query agent run logs for diagnostics (expands to sources:[{tool:'agent_logs', input:{...}}]). Value: 'all' for recent logs, 'tools' for tool calls only, 'errors' for errors only, or a specific run_id",
			},
			"gateway_logs": map[string]any{
				"type":        "string",
				"description": "Shortcut: query gateway process logs (expands to sources:[{tool:'gateway_logs', input:{...}}]). Value: 'all' for recent 100 lines, 'errors' for ERR only, 'warnings' for WRN+ERR, or a package name (e.g. 'chat', 'server', 'telegram') to filter by package",
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
			"output_format": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "json", "list"},
				"description": "Desired output format (default: text)",
			},
			"max_length": map[string]any{
				"type":        "string",
				"enum":        []string{"brief", "normal", "detailed"},
				"description": "Output length hint: brief (~500 chars, fits Telegram), normal (default), detailed (thorough analysis)",
			},
			"chain": map[string]any{
				"type":        "boolean",
				"description": "If true, pilot may request one follow-up round of tool calls based on initial analysis (e.g., read files found by grep)",
			},
		},
		"required": []string{"task"},
	}
}

// --- Main pilot function ---

// toolPilot creates the pilot ToolFunc. It uses the ToolExecutor to run
// source tools from the registry before feeding results to the local LLM.
func toolPilot(tools ToolExecutor, workspaceDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		start := time.Now()
		logger := slog.Default()

		var p pilotParams
		if err := jsonutil.UnmarshalInto("pilot params", input, &p); err != nil {
			return "", err
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
			gathered = append(gathered, sourceResult{"content", p.Content, "content"})
		}
		for i, item := range p.Items {
			gathered = append(gathered, sourceResult{fmt.Sprintf("item[%d]", i+1), item, "content"})
		}

		// Check sglang health before LLM call.
		if !checkSglangHealth() {
			result := buildFallbackResult(p.Task, gathered)
			logger.Warn("pilot: sglang unavailable, returning raw results",
				"task", p.Task,
				"sources", len(gathered),
			)
			return result, nil
		}

		// Determine thinking mode and max tokens.
		// Brief mode disables thinking — not enough token budget for both.
		thinking := p.MaxLength != "brief" && shouldUseThinking(p.Task, len(sources))
		maxTokens := pilotMaxTokens
		if thinking {
			maxTokens = 6144 // extra budget for thinking + answer
		} else if p.MaxLength == "brief" {
			maxTokens = 1024
		}

		// Phase 2: Build prompt and call local LLM.
		systemPrompt := buildPilotSystemPrompt(workspaceDir, thinking)
		userMsg := buildPilotPrompt(p.Task, p.OutputFormat, p.MaxLength, gathered)

		result, err := callLocalLLM(ctx, systemPrompt, userMsg, maxTokens)
		if err != nil {
			// Graceful degradation: return raw tool results if LLM fails.
			logger.Warn("pilot: LLM call failed, falling back to raw results",
				"error", err,
				"task", p.Task,
			)
			return buildFallbackResult(p.Task, gathered), nil
		}

		// Strip thinking tags from response.
		if thinking {
			result = strings.TrimSpace(jsonutil.StripThinkingTags(result))
		}

		// Phase 3 (optional): Chaining — let LLM request follow-up tool calls.
		if p.Chain && len(result) > 0 {
			chainResult := executeChain(ctx, result, p.Task, p.OutputFormat, p.MaxLength, tools, workspaceDir, logger)
			if chainResult != "" {
				result = chainResult
			}
		}

		// Post-process output based on format.
		result = postProcessOutput(result, p.OutputFormat, p.MaxLength)

		// Metrics logging.
		totalInput := 0
		for _, g := range gathered {
			totalInput += len(g.content)
		}
		logger.Info("pilot: completed",
			"task_len", len(p.Task),
			"sources", len(sources),
			"input_chars", totalInput,
			"output_chars", len(result),
			"thinking", thinking,
			"chain", p.Chain,
			"elapsed", time.Since(start).Round(time.Millisecond),
		)

		return result, nil
	}
}

// --- Types ---

// pilotParams is the parsed tool input.
type pilotParams struct {
	Task         string            `json:"task"`
	Sources      []sourceSpec      `json:"sources"`
	Content      string            `json:"content"`
	Items        []string          `json:"items"`
	OutputFormat string            `json:"output_format"`
	MaxLength    string            `json:"max_length"`
	Chain        bool              `json:"chain"`
	PostProcess  []postProcessStep `json:"post_process"`

	// Shortcuts.
	File      string   `json:"file"`
	Files     []string `json:"files"`
	Exec      string   `json:"exec"`
	Grep      string   `json:"grep"`
	Find      string   `json:"find"`
	Path      string   `json:"path"`
	URL       string   `json:"url"`
	HTTP      string   `json:"http"`
	Diff      string   `json:"diff"`
	Test      string   `json:"test"`
	Tree      string   `json:"tree"`
	GitLog    string   `json:"git_log"`
	Health    bool     `json:"health"`
	KVKey     string   `json:"kv_key"`
	Memory    string   `json:"memory"`
	Gmail     string   `json:"gmail"`
	YouTube   string   `json:"youtube"`
	Polaris   string   `json:"polaris"`
	Image     string   `json:"image"`
	Ls        string   `json:"ls"`
	Vega        string `json:"vega"`
	AgentLogs   string `json:"agent_logs"`
	GatewayLogs string `json:"gateway_logs"`
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
	label      string
	content    string
	sourceType string // "file", "exec", "grep", "find", "url", "content"
}

// --- Shortcut expansion ---

// expandShortcuts converts convenience params (file, exec, grep, find, url) into sourceSpecs.
