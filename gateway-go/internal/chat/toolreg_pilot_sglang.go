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
