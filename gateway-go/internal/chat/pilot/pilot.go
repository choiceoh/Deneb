package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Pilot tool: the AI agent's fast helper that can orchestrate other tools.
//
// The agent specifies a task and data sources. Pilot:
//  1. Executes source tool calls via the ToolRegistry (parallel, per-source timeout)
//  2. Feeds all gathered data + task to the pilot model role
//  3. Optionally chains: if chain=true, LLM can request follow-up tool calls
//  4. Returns the result synchronously
//
// Shortcuts (file, exec, grep, find, url) expand to sources internally for convenience.

const (
	pilotTimeout     = 2 * time.Minute
	pilotMaxInput    = 24000 // chars — auto-truncate beyond this
	pilotMaxTokens   = 4096
	pilotMaxSources  = 10
	sourceTimeout    = 30 * time.Second // per-source tool execution timeout
	sglangHealthTTL  = 30 * time.Second
	sglangWarmupTTL  = 5 * time.Second
	sglangWarmupFor  = 1 * time.Minute
	sglangHealthPing = 3 * time.Second // HTTP timeout for health check
)

// ToolExecutor is an alias for toolctx.ToolExecutor, the canonical definition.
type ToolExecutor = toolctx.ToolExecutor

// --- Thinking mode for pilot analysis ---

// thinkingKeywords triggers thinking mode when the task contains complex analysis keywords.
var thinkingKeywords = []string{
	"분석", "비교", "리뷰", "디버그", "문제", "원인", "검토",
	"analyze", "compare", "review", "debug", "diagnose", "investigate", "diff",
}

var pilotSimpleSourceTools = map[string]bool{
	"find":         true,
	"git":          true,
	"gmail":        true,
	"grep":         true,
	"health_check": true,
	"http":         true,
	"kv":           true,
	"memory":       true,
	"polaris":      true,
	"read":         true,
	"tree":         true,
	"web_fetch":    true,
}

// ShouldUseThinking decides whether to enable Qwen3.5 thinking mode based on
// task complexity (keywords) and number of sources.
func ShouldUseThinking(task string, sourceCount int) bool {
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

// ShouldBypassPilotLLM returns true when sources are simple enough to skip LLM.
func ShouldBypassPilotLLM(p PilotParams, sources []SourceSpec, gathered []SourceResult) bool {
	if len(sources) == 0 || len(sources) > 2 || len(gathered) < len(sources) {
		return false
	}
	if p.Chain || len(p.PostProcess) > 0 || p.Content != "" || len(p.Items) > 0 {
		return false
	}

	totalChars := 0
	for i, src := range sources {
		if src.OnlyIf != "" || src.SkipIf != "" {
			return false
		}
		if !pilotSimpleSourceTools[src.Tool] {
			return false
		}
		totalChars += len(gathered[i].Content)
	}

	return totalChars > 0 && totalChars <= 1000
}

// BuildPilotPassthroughResult formats gathered results for direct passthrough.
func BuildPilotPassthroughResult(gathered []SourceResult) string {
	if len(gathered) == 0 {
		return ""
	}
	if len(gathered) == 1 {
		return gathered[0].Content
	}

	var sb strings.Builder
	for i, g := range gathered {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("--- ")
		sb.WriteString(g.Label)
		sb.WriteString(" ---\n")
		sb.WriteString(g.Content)
	}
	return sb.String()
}

// --- System prompt ---

// BuildPilotSystemPrompt builds the system prompt for pilot LLM calls.
func BuildPilotSystemPrompt(workspaceDir string, thinking bool) string {
	var sb strings.Builder
	sb.WriteString(`You are Pilot, a fast AI assistant. Your output goes to Telegram (4096 char limit).
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

// --- Main pilot function ---

// ToolPilot creates the pilot ToolFunc. It uses the ToolExecutor to run
// source tools from the registry before feeding results to the pilot LLM.
func ToolPilot(tools ToolExecutor, workspaceDir string) func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		start := time.Now()
		logger := slog.Default()

		var p PilotParams
		if err := jsonutil.UnmarshalInto("pilot params", input, &p); err != nil {
			return "", err
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		// Expand shortcuts into source specs.
		sources := ExpandShortcuts(p)

		// Merge with explicit sources.
		sources = append(sources, p.Sources...)

		// Cap source count.
		if len(sources) > pilotMaxSources {
			sources = sources[:pilotMaxSources]
		}

		// Phase 1: Execute sources (unconditional in parallel, then conditional).
		gathered := ExecuteSources(ctx, sources, tools)

		// Phase 1.5: Apply post-processing steps to gathered data.
		if len(p.PostProcess) > 0 {
			gathered = ApplyPostProcessSteps(gathered, p.PostProcess)
		}

		// Add direct content/items.
		if p.Content != "" {
			gathered = append(gathered, SourceResult{"content", p.Content, "content"})
		}
		for i, item := range p.Items {
			gathered = append(gathered, SourceResult{fmt.Sprintf("item[%d]", i+1), item, "content"})
		}

		if ShouldBypassPilotLLM(p, sources, gathered) {
			result := BuildPilotPassthroughResult(gathered)
			logger.Info("pilot: bypassed local llm for simple source set",
				"task", p.Task,
				"sources", len(sources),
				"chars", len(result),
			)
			return result, nil
		}

		// Determine thinking mode and max tokens.
		// Brief mode disables thinking — not enough token budget for both.
		thinking := p.MaxLength != "brief" && ShouldUseThinking(p.Task, len(sources))
		maxTokens := pilotMaxTokens
		if thinking {
			maxTokens = 6144 // extra budget for thinking + answer
		} else if p.MaxLength == "brief" {
			maxTokens = 1024
		}

		// Phase 2: Build prompt and call the pilot LLM.
		systemPrompt := BuildPilotSystemPrompt(workspaceDir, thinking)
		userMsg := BuildPilotPrompt(p.Task, p.OutputFormat, p.MaxLength, gathered)

		result, err := CallLocalLLM(ctx, systemPrompt, userMsg, maxTokens)
		if err != nil {
			// Graceful degradation: return raw tool results if LLM fails.
			logger.Warn("pilot: LLM call failed, falling back to raw results",
				"error", err,
				"task", p.Task,
			)
			return BuildFallbackResult(p.Task, gathered), nil
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
		result = PostProcessOutput(result, p.OutputFormat, p.MaxLength)

		// Metrics logging.
		totalInput := 0
		for _, g := range gathered {
			totalInput += len(g.Content)
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

// PilotParams is the parsed tool input.
type PilotParams struct {
	Task         string          `json:"task"`
	Sources      []SourceSpec    `json:"sources"`
	Content      string          `json:"content"`
	Items        []string        `json:"items"`
	OutputFormat string          `json:"output_format"`
	MaxLength    string          `json:"max_length"`
	Chain        bool            `json:"chain"`
	PostProcess  []PostProcessStep `json:"post_process"`

	// Shortcuts.
	File        string   `json:"file"`
	Files       []string `json:"files"`
	Exec        string   `json:"exec"`
	Grep        string   `json:"grep"`
	Find        string   `json:"find"`
	Path        string   `json:"path"`
	URL         string   `json:"url"`
	HTTP        string   `json:"http"`
	Diff        string   `json:"diff"`
	Test        string   `json:"test"`
	Tree        string   `json:"tree"`
	GitLog      string   `json:"git_log"`
	Health      bool     `json:"health"`
	KVKey       string   `json:"kv_key"`
	Memory      string   `json:"memory"`
	Gmail       string   `json:"gmail"`
	YouTube     string   `json:"youtube"`
	Polaris     string   `json:"polaris"`
	Image       string   `json:"image"`
	Ls          string   `json:"ls"`
	AgentLogs   string   `json:"agent_logs"`
	GatewayLogs string   `json:"gateway_logs"`
}

// PostProcessStep is a programmatic transformation applied to gathered data.
type PostProcessStep struct {
	Action string `json:"action"` // filter_lines, head, tail, unique, sort
	Param  string `json:"param"`  // action-specific parameter
}

// SourceSpec is a tool call specification from the agent.
type SourceSpec struct {
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
	Label  string          `json:"label"`
	OnlyIf string          `json:"only_if"` // execute only if named source succeeded
	SkipIf string          `json:"skip_if"` // skip if named source succeeded
}

// SourceResult is a labeled chunk of gathered data.
type SourceResult struct {
	Label      string
	Content    string
	SourceType string // "file", "exec", "grep", "find", "url", "content"
}
