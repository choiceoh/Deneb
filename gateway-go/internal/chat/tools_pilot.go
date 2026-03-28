package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
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
			"ls": map[string]any{
				"type":        "string",
				"description": "Shortcut: list directory contents (expands to sources:[{tool:'ls', input:{path:...}}])",
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

	if p.Find != "" {
		findInput := map[string]any{"pattern": p.Find}
		if p.Path != "" {
			findInput["path"] = p.Path
		}
		specs = append(specs, sourceSpec{
			Tool:  "find",
			Input: mustJSON(findInput),
			Label: "find: " + p.Find,
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

	if p.Diff != "" {
		diffInput := map[string]any{}
		switch p.Diff {
		case "all":
			diffInput["action"] = "all"
		case "staged":
			diffInput["action"] = "staged"
		case "unstaged":
			diffInput["action"] = "unstaged"
		default:
			// Treat as a commit hash.
			diffInput["action"] = "commit"
			diffInput["commit"] = p.Diff
		}
		specs = append(specs, sourceSpec{
			Tool:  "diff",
			Input: mustJSON(diffInput),
			Label: "diff: " + p.Diff,
		})
	}

	if p.Test != "" {
		testInput := map[string]any{"action": "run"}
		if p.Test != "all" {
			testInput["path"] = p.Test
		}
		specs = append(specs, sourceSpec{
			Tool:  "test",
			Input: mustJSON(testInput),
			Label: "test: " + p.Test,
		})
	}

	if p.Tree != "" {
		specs = append(specs, sourceSpec{
			Tool:  "tree",
			Input: mustJSON(map[string]any{"path": p.Tree, "depth": 3}),
			Label: "tree: " + p.Tree,
		})
	}

	if p.GitLog != "" {
		gitInput := map[string]any{"action": "log"}
		switch p.GitLog {
		case "oneline":
			gitInput["oneline"] = true
			gitInput["count"] = 30
		case "recent":
			gitInput["count"] = 20
		default:
			// Treat as a count if numeric, otherwise default.
			gitInput["count"] = 20
		}
		specs = append(specs, sourceSpec{
			Tool:  "git",
			Input: mustJSON(gitInput),
			Label: "git_log: " + p.GitLog,
		})
	}

	if p.Health {
		specs = append(specs, sourceSpec{
			Tool:  "health_check",
			Input: mustJSON(map[string]any{}),
			Label: "health_check",
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

	if p.Gmail != "" {
		specs = append(specs, sourceSpec{
			Tool:  "gmail",
			Input: mustJSON(map[string]any{"action": "search", "query": p.Gmail}),
			Label: "gmail: " + p.Gmail,
		})
	}

	if p.YouTube != "" {
		specs = append(specs, sourceSpec{
			Tool:  "youtube_transcript",
			Input: mustJSON(map[string]any{"url": p.YouTube}),
			Label: "youtube: " + p.YouTube,
		})
	}

	if p.Polaris != "" {
		specs = append(specs, sourceSpec{
			Tool:  "polaris",
			Input: mustJSON(map[string]any{"action": "search", "query": p.Polaris}),
			Label: "polaris: " + p.Polaris,
		})
	}

	if p.Image != "" {
		specs = append(specs, sourceSpec{
			Tool:  "image",
			Input: mustJSON(map[string]any{"paths": []string{p.Image}}),
			Label: "image: " + p.Image,
		})
	}

	if p.Ls != "" {
		specs = append(specs, sourceSpec{
			Tool:  "ls",
			Input: mustJSON(map[string]any{"path": p.Ls}),
			Label: "ls: " + p.Ls,
		})
	}

	if p.Vega != "" {
		specs = append(specs, sourceSpec{
			Tool:  "vega",
			Input: mustJSON(map[string]any{"query": p.Vega}),
			Label: "vega: " + p.Vega,
		})
	}

	if p.AgentLogs != "" {
		input := map[string]any{"limit": 50}
		switch p.AgentLogs {
		case "all":
			// No filter — return recent logs.
		case "tools":
			input["type"] = "turn.tool"
		case "errors":
			input["type"] = "run.error"
		default:
			// Treat as a specific run_id.
			input["run_id"] = p.AgentLogs
		}
		specs = append(specs, sourceSpec{
			Tool:  "agent_logs",
			Input: mustJSON(input),
			Label: "agent_logs: " + p.AgentLogs,
		})
	}

	if p.GatewayLogs != "" {
		input := map[string]any{"lines": 100}
		switch p.GatewayLogs {
		case "all":
			// No filter — return recent lines.
		case "errors":
			input["level"] = "error"
		case "warnings":
			input["level"] = "warn"
		default:
			// Treat as a package name filter.
			input["pkg"] = p.GatewayLogs
		}
		specs = append(specs, sourceSpec{
			Tool:  "gateway_logs",
			Input: mustJSON(input),
			Label: "gateway_logs: " + p.GatewayLogs,
		})
	}

	return specs
}

// --- Source execution ---

// sourceTypeFromTool maps tool name to source type for smart truncation.
func sourceTypeFromTool(tool string) string {
	switch tool {
	case "read":
		return "file"
	case "exec":
		return "exec"
	case "grep":
		return "grep"
	case "find":
		return "find"
	case "web_fetch":
		return "url"
	case "ls", "diff", "tree":
		return "file"
	case "agent_logs", "gateway_logs", "test", "http":
		return "exec"
	case "gmail", "youtube_transcript", "polaris", "image", "vega":
		return "content"
	default:
		return "content"
	}
}

// executeSources runs source tool calls via the ToolRegistry.
// Unconditional sources (no only_if/skip_if) run in parallel with per-source timeout.
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
				label:      is.src.Label,
				content:    "[error: pilot cannot call itself]",
				sourceType: "content",
			}
			continue
		}
		wg.Add(1)
		go func(idx int, s sourceSpec) {
			defer wg.Done()
			srcCtx, srcCancel := context.WithTimeout(ctx, sourceTimeout)
			defer srcCancel()
			output, err := tools.Execute(srcCtx, s.Tool, s.Input)
			if err != nil {
				results[idx] = sourceResult{s.Label, fmt.Sprintf("[tool error: %s]", err), sourceTypeFromTool(s.Tool)}
				return
			}
			results[idx] = sourceResult{s.Label, output, sourceTypeFromTool(s.Tool)}
		}(is.idx, is.src)
	}
	wg.Wait()

	// Phase 2: Run conditional sources sequentially.
	for _, is := range conditional {
		src := is.src
		if src.Tool == "pilot" {
			results[is.idx] = sourceResult{src.Label, "[error: pilot cannot call itself]", "content"}
			continue
		}
		if src.OnlyIf != "" && !sourceSucceeded(results, src.OnlyIf) {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[skipped: %q did not succeed]", src.OnlyIf), "content"}
			continue
		}
		if src.SkipIf != "" && sourceSucceeded(results, src.SkipIf) {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[skipped: %q succeeded]", src.SkipIf), "content"}
			continue
		}
		srcCtx, srcCancel := context.WithTimeout(ctx, sourceTimeout)
		output, err := tools.Execute(srcCtx, src.Tool, src.Input)
		srcCancel()
		if err != nil {
			results[is.idx] = sourceResult{src.Label, fmt.Sprintf("[tool error: %s]", err), sourceTypeFromTool(src.Tool)}
			continue
		}
		results[is.idx] = sourceResult{src.Label, output, sourceTypeFromTool(src.Tool)}
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

// --- Prompt building ---

// buildPilotPrompt assembles the user message from task + gathered data.
func buildPilotPrompt(task, outputFormat, maxLength string, blocks []sourceResult) string {
	var sb strings.Builder

	sb.WriteString("Task: ")
	sb.WriteString(task)

	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
	}

	if maxLength != "" && maxLength != "normal" {
		sb.WriteString("\nOutput length: ")
		switch maxLength {
		case "brief":
			sb.WriteString("Keep response under 500 characters. Be extremely concise.")
		case "detailed":
			sb.WriteString("Provide thorough, detailed analysis.")
		}
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
		sb.WriteString(smartTruncate(b.content, perBlock, b.sourceType))
	}

	return sb.String()
}

// --- Smart truncation ---

// smartTruncate truncates content based on source type:
//   - file: preserves beginning (60%) + end (40%) for code context
//   - exec: preserves end (80%) — errors/results at the bottom
//   - default: simple head truncation
func smartTruncate(s string, maxChars int, sourceType string) string {
	if len(s) <= maxChars {
		return s
	}

	marker := fmt.Sprintf("\n\n[... truncated, original %d chars ...]\n\n", len(s))

	budget := maxChars - len(marker)
	if budget < 200 {
		// Not enough room for head+tail split — fall back to simple head truncation.
		return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
	}

	switch sourceType {
	case "file":
		// Preserve start + end for file content (function signatures + tail).
		headSize := budget * 6 / 10
		tailSize := budget - headSize
		// Ensure head+tail don't exceed content length (when s is only slightly over maxChars).
		if headSize+tailSize >= len(s) {
			return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
		}
		return s[:headSize] + marker + s[len(s)-tailSize:]

	case "exec":
		// Preserve end for command output (errors/results typically at bottom).
		headSize := budget * 2 / 10
		if headSize < 200 {
			headSize = 200
		}
		tailSize := budget - headSize
		if headSize+tailSize >= len(s) {
			return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
		}
		return s[:headSize] + marker + s[len(s)-tailSize:]

	default:
		return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
	}
}

// --- JSON output cleaning ---

// cleanJSONResponse strips markdown fences and validates JSON output.
// If the output is not valid JSON, tries to extract the first JSON object/array.
func cleanJSONResponse(s string) string {
	s = strings.TrimSpace(s)

	// Strip markdown code fences.
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}

	if json.Valid([]byte(s)) {
		return s
	}

	// Try to extract the first JSON object or array.
	if idx := strings.IndexAny(s, "[{"); idx >= 0 {
		candidate := s[idx:]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return s
}

// --- Output post-processing ---

// Hard limits for output length enforcement.
const (
	briefMaxChars    = 500
	detailedMaxChars = 8000
)

// postProcessOutput applies format-specific cleaning and length enforcement.
func postProcessOutput(result, outputFormat, maxLength string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return result
	}

	// Format-specific cleaning.
	switch outputFormat {
	case "json":
		result = cleanJSONResponse(result)
	case "list":
		result = cleanListResponse(result)
	default:
		result = normalizeMarkdown(result)
	}

	// Hard length enforcement — LLM hints are unreliable.
	switch maxLength {
	case "brief":
		result = enforceMaxLength(result, briefMaxChars)
	case "detailed":
		// Allow longer output but still cap at reasonable limit.
		result = enforceMaxLength(result, detailedMaxChars)
	}

	return result
}

// cleanListResponse normalizes numbered list output from the LLM.
// Ensures consistent numbering and removes non-list preamble.
func cleanListResponse(s string) string {
	lines := strings.Split(s, "\n")
	var listLines []string
	var preface []string
	inList := false
	num := 1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inList {
				listLines = append(listLines, "")
			}
			continue
		}

		// Detect list items: "1.", "2.", "- ", "* ", etc.
		if isListItem(trimmed) {
			inList = true
			// Re-number for consistency.
			content := stripListPrefix(trimmed)
			listLines = append(listLines, fmt.Sprintf("%d. %s", num, content))
			num++
		} else if inList {
			// Continuation line within list — append to last item.
			if len(listLines) > 0 {
				listLines[len(listLines)-1] += " " + trimmed
			}
		} else {
			preface = append(preface, trimmed)
		}
	}

	if len(listLines) == 0 {
		return s // No list found, return as-is.
	}

	// Include preface if it's brief (1-2 lines), otherwise drop it.
	var sb strings.Builder
	if len(preface) <= 2 {
		for _, p := range preface {
			sb.WriteString(p)
			sb.WriteByte('\n')
		}
		if len(preface) > 0 {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString(strings.Join(listLines, "\n"))
	return strings.TrimSpace(sb.String())
}

// isListItem checks if a line starts with a list marker.
func isListItem(s string) bool {
	if len(s) < 2 {
		return false
	}
	// Numbered: "1. ", "2. ", etc.
	if s[0] >= '0' && s[0] <= '9' {
		for i := 1; i < len(s); i++ {
			if s[i] == '.' && i+1 < len(s) && s[i+1] == ' ' {
				return true
			}
			if s[i] < '0' || s[i] > '9' {
				break
			}
		}
	}
	// Bullet: "- " or "* "
	if (s[0] == '-' || s[0] == '*') && s[1] == ' ' {
		return true
	}
	return false
}

// stripListPrefix removes the list marker from a line.
func stripListPrefix(s string) string {
	// Numbered: "1. content" → "content"
	if s[0] >= '0' && s[0] <= '9' {
		for i := 1; i < len(s); i++ {
			if s[i] == '.' && i+1 < len(s) && s[i+1] == ' ' {
				return strings.TrimSpace(s[i+2:])
			}
			if s[i] < '0' || s[i] > '9' {
				break
			}
		}
	}
	// Bullet: "- content" or "* content"
	if (s[0] == '-' || s[0] == '*') && len(s) > 1 && s[1] == ' ' {
		return strings.TrimSpace(s[2:])
	}
	return s
}

// normalizeMarkdown fixes common Qwen3.5 markdown issues:
//   - closes unclosed code blocks
//   - collapses 3+ consecutive blank lines to 2
//   - trims trailing whitespace per line
func normalizeMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blankCount := 0
	codeBlockOpen := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")

		// Track code block state.
		if strings.HasPrefix(trimmed, "```") {
			codeBlockOpen = !codeBlockOpen
			blankCount = 0
			out = append(out, trimmed)
			continue
		}

		// Collapse excessive blank lines (max 2 consecutive).
		if trimmed == "" {
			blankCount++
			if blankCount <= 2 {
				out = append(out, "")
			}
			continue
		}

		blankCount = 0
		out = append(out, trimmed)
	}

	// Close unclosed code block.
	if codeBlockOpen {
		out = append(out, "```")
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// enforceMaxLength hard-truncates output to maxChars, cutting at the last
// complete line or sentence boundary.
func enforceMaxLength(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}

	// Try to cut at a line boundary.
	cut := s[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > maxChars/2 {
		return strings.TrimSpace(cut[:idx]) + "\n…"
	}

	// Try to cut at a sentence boundary.
	for _, sep := range []string{". ", "。", "! ", "? "} {
		if idx := strings.LastIndex(cut, sep); idx > maxChars/2 {
			return cut[:idx+len(sep)] + "…"
		}
	}

	// Hard cut at maxChars.
	return strings.TrimSpace(cut) + "…"
}

// --- Chaining ---

const chainSystemPrompt = `You are a tool call planner.
Given the initial analysis, decide if follow-up tool calls would improve the answer.
If yes, return ONLY a JSON array of tool calls: [{"tool":"...", "input":{...}, "label":"..."}]
If no follow-up is needed, return exactly: DONE
No other text.`

// executeChain performs one follow-up round of tool calls if the LLM requests them.
func executeChain(ctx context.Context, initialResult, task, outputFormat, maxLength string, tools ToolExecutor, workspaceDir string, logger *slog.Logger) string {
	// Ask LLM if follow-up tools are needed.
	prompt := fmt.Sprintf("Task: %s\n\nInitial analysis:\n%s\n\nDo you need to call any follow-up tools to improve this answer?",
		task, truncateHead(initialResult, 4000))

	decision, err := callLocalLLM(ctx, chainSystemPrompt, prompt, 1024)
	if err != nil {
		logger.Debug("pilot chain: planning failed", "error", err)
		return ""
	}

	decision = strings.TrimSpace(decision)
	if decision == "DONE" || decision == "" {
		return ""
	}

	// Parse follow-up tool calls.
	var followUp []sourceSpec
	if err := json.Unmarshal([]byte(decision), &followUp); err != nil {
		logger.Debug("pilot chain: invalid tool calls JSON", "error", err, "raw", decision)
		return ""
	}

	if len(followUp) == 0 || len(followUp) > 5 {
		return ""
	}

	// Filter out any self-referential pilot calls from chain.
	filtered := followUp[:0]
	for _, f := range followUp {
		if f.Tool != "pilot" {
			filtered = append(filtered, f)
		}
	}
	followUp = filtered
	if len(followUp) == 0 {
		return ""
	}

	// Execute follow-up tools.
	chainGathered := executeSources(ctx, followUp, tools)

	// Final synthesis with all data.
	var sb strings.Builder
	sb.WriteString("Task: ")
	sb.WriteString(task)
	sb.WriteString("\n\nInitial analysis:\n")
	sb.WriteString(truncateHead(initialResult, 4000))
	sb.WriteString("\n\nFollow-up data:\n")
	for _, g := range chainGathered {
		sb.WriteString("\n--- ")
		sb.WriteString(g.label)
		sb.WriteString(" ---\n")
		sb.WriteString(smartTruncate(g.content, 4000, g.sourceType))
	}
	sb.WriteString("\n\nNow provide the final comprehensive answer incorporating all data.")

	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
	}

	if maxLength == "brief" {
		sb.WriteString("\nKeep response under 500 characters.")
	}

	systemPrompt := buildPilotSystemPrompt(workspaceDir, false)
	final, err := callLocalLLM(ctx, systemPrompt, sb.String(), pilotMaxTokens)
	if err != nil {
		logger.Debug("pilot chain: final synthesis failed", "error", err)
		return ""
	}

	logger.Info("pilot chain: completed",
		"follow_up_tools", len(followUp),
		"final_chars", len(final),
	)

	return final
}

// --- Fallback (sglang unavailable) ---

// buildFallbackResult formats raw tool results when sglang is not available.
func buildFallbackResult(task string, gathered []sourceResult) string {
	var sb strings.Builder
	sb.WriteString("[pilot: sglang 서버에 연결할 수 없어 원본 결과를 반환합니다]\n\n")
	sb.WriteString("Task: ")
	sb.WriteString(task)

	if len(gathered) == 0 {
		return sb.String()
	}

	for _, g := range gathered {
		sb.WriteString("\n\n--- ")
		sb.WriteString(g.label)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateHead(g.content, 3000))
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

// truncateInput is a simple head-only truncation. Used by sglang_hooks.go and pilot fallback.
func truncateInput(s string, maxChars int) string {
	return truncateHead(s, maxChars)
}

// truncateHead is a simple head-only truncation (used for chain prompts, fallback).
func truncateHead(s string, maxChars int) string {
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

// sglangClient is a singleton LLM client for the local sglang server.
// Avoids creating a new HTTP client + transport on every call.
var (
	sglangClientOnce sync.Once
	sglangClientInst *llm.Client
)

func getSglangClient() *llm.Client {
	sglangClientOnce.Do(func() {
		sglangClientInst = llm.NewClient(defaultSglangBaseURL, "", llm.WithLogger(slog.Default()))
	})
	return sglangClientInst
}

func callLocalLLM(ctx context.Context, system, userMessage string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := getSglangClient()

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
