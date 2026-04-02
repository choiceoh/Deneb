package chat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
)

// sglang_hooks.go — local sglang model hooks into the agent pipeline:
//
//  1. Proactive Context: before agent run, scan related files/memory to enrich system prompt
//  2. Tool Output Compression: after tool execution, compress large outputs
//  3. Auto Memory: after successful run, extract key learnings to MEMORY.md

// --- 1. Proactive Context ---
// Injected in executeAgentRun, between context assembly and agent loop.
// The local model analyzes the user's message and gathers relevant context.

const (
	proactiveTimeout       = 15 * time.Second // local sglang: optimal timeout (tested: 35→20→15→10, 15 is sweet spot)
	proactiveRemoteTimeout = 20 * time.Second // remote fallback (Gemini Flash) needs more time
	proactiveMaxTokens     = 1024
	proactiveMinMsgLen     = 20 // skip for very short messages
)

const proactiveSystemPrompt = `You are a context preparation assistant.
Given the user's message and workspace info, identify what context would help answer it.
Return a brief context note (max 5 lines) with:
- Relevant file names or paths the main AI should look at
- Related past decisions from memory (if any)
- Key technical context to keep in mind
Reply in Korean. Be extremely concise. If no special context is needed, reply with just "N/A".`

// buildProactiveContext uses the local sglang model to analyze the user's
// message and generate a context hint for the main agent.
// Returns empty string if proactive context is not needed or fails.
// isLowInfoMessage returns true for short follow-up messages that don't benefit
// from proactive context (e.g., "응", "좋아 그렇게 해", "계속", "다음은?").
// Uses rune count and simple keyword heuristics to avoid unnecessary sglang calls.
func isLowInfoMessage(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	runes := []rune(trimmed)
	runeCount := len(runes)
	// Very short messages (< 8 runes) are almost always follow-ups.
	if runeCount < 8 {
		return true
	}
	// Short messages are only treated as low-info when they look like pure
	// acknowledgements/continuations and do not contain obvious task intent.
	// This avoids skipping concrete imperative asks such as
	// "로그 보고 원인 분석해줘" that may not end with a question mark.
	if runeCount < 30 && !strings.ContainsAny(trimmed, "?？") {
		if containsTaskIntent(trimmed) {
			return false
		}
		return true
	}
	return false
}

// containsTaskIntent returns true when the message includes clear ask/action
// signals. This keeps proactive context enabled for concise but actionable
// requests.
func containsTaskIntent(msg string) bool {
	lower := strings.ToLower(msg)
	keywords := []string{
		// Korean ask/action patterns.
		"해줘", "해주세요", "해 줄", "해봐", "해 봐", "확인", "분석", "조사", "정리", "수정", "고쳐",
		"원인", "왜", "어떻게", "찾아", "비교", "설명", "검토", "테스트", "추가", "삭제", "리팩토링",
		"튜닝", "개선", "최적화", "해결", "보여", "알려",
		// English ask/action patterns.
		"please", "fix", "debug", "analyze", "investigate", "check", "review",
		"compare", "explain", "summarize", "optimize", "improve", "why", "how",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func buildProactiveContext(ctx context.Context, userMessage, workspaceDir string, logger *slog.Logger) string {
	if len(userMessage) < proactiveMinMsgLen {
		return ""
	}
	if isLowInfoMessage(userMessage) {
		return ""
	}
	// Check sglang health (cached probe, no per-call overhead).
	sglangUp := pilot.CheckSglangHealth()
	if !sglangUp && !pilot.HasRegistry() {
		return ""
	}

	timeout := proactiveTimeout
	if !sglangUp {
		timeout = proactiveRemoteTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Gather workspace signals: recent file list + memory file snippets.
	var contextInfo strings.Builder
	contextInfo.WriteString("User message: ")
	contextInfo.WriteString(userMessage)

	// List workspace top-level files for orientation.
	if entries, err := os.ReadDir(workspaceDir); err == nil {
		contextInfo.WriteString("\n\nWorkspace files: ")
		names := make([]string, 0, 20)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
			if len(names) >= 20 {
				break
			}
		}
		contextInfo.WriteString(strings.Join(names, ", "))
	}

	// Memory content is provided to the main LLM by PrefetchKnowledge (importance-weighted).
	// Reading MEMORY.md here would be redundant I/O on every message.

	var result string
	var err error
	if sglangUp {
		result, err = pilot.CallLocalLLM(ctx, proactiveSystemPrompt, contextInfo.String(), proactiveMaxTokens)
	} else {
		// sglang down — use pilot model (Gemini Flash) for proactive context.
		result, err = pilot.CallPilotLLM(ctx, proactiveSystemPrompt, contextInfo.String(), proactiveMaxTokens)
	}
	if err != nil {
		logger.Debug("proactive context failed", "error", err, "remote", !sglangUp)
		return ""
	}

	result = strings.TrimSpace(result)
	if result == "" || result == "N/A" || strings.ToLower(result) == "n/a" {
		return ""
	}

	return result
}

// deferredProactiveHint returns a DeferredSystemText function that non-blocking
// reads the proactive channel. Returns the hint text when ready, empty string
// while waiting, or signals done (empty hint consumed / hint delivered) so the
// executor clears the hook and stops calling it.
func deferredProactiveHint(ch <-chan string, start time.Time, logger *slog.Logger) func() string {
	var consumed bool
	return func() string {
		if consumed {
			return ""
		}
		select {
		case hint := <-ch:
			consumed = true
			if hint != "" {
				logger.Info("proactive context hit (deferred injection)",
					"chars", len(hint),
					"elapsedMs", time.Since(start).Milliseconds())
				return "\n## Context Hint (from local analysis)\n" + hint
			}
		default:
		}
		return ""
	}
}

// --- 2. Tool Output Compression ---
// Called in the agent loop after tool execution, before feeding results back to LLM.

const (
	compressThreshold = 16000 // chars — only compress very large outputs (saves sglang calls)
	compressMaxTokens = 1024
	compressTimeout   = 20 * time.Second
	// Tools whose output should never be compressed (they're already structured/small).
	toolCompressSkipPrefix = "pilot" // pilot already uses sglang, don't double-process
)

// toolCompressSkipSet contains tools whose output should not be compressed.
// Two categories:
//   - Already-structured outputs (grep, find, tree, git, analyze, diff): file:line:match
//     or directory-tree format; LLM compression loses structure and is slower than
//     the existing GrepResultSummarizer / OutputTrimmer pipeline.
//   - Tools that already use sglang internally (pilot) or return small JSON (kv,
//     sessions_list) where compression adds no value.
var toolCompressSkipSet = map[string]bool{
	// Structured-output tools — already handled by post-processors.
	"grep":    true,
	"find":    true,
	"tree":    true,
	"git":     true,
	"analyze": true,
	"diff":    true,
	// Internal / already-small tools.
	"pilot":         true,
	"memory": true,
	"kv":     true,
	"sessions_list": true,
}

const compressSystemPrompt = `You are a tool output compressor.
Condense the tool output to its essential information. Preserve:
- Error messages and exit codes
- Key data points and numbers
- File paths and line numbers
- Important patterns and findings
Remove verbose boilerplate, repeated lines, and padding.
Keep the same language. Be concise but don't lose critical details.
Max 30 lines.`

// compressToolOutput shrinks a large tool output using the local sglang model.
// Returns the original output if compression is not needed or fails.
func compressToolOutput(ctx context.Context, toolName, output string, logger *slog.Logger) string {
	if len(output) < compressThreshold {
		return output
	}
	if toolCompressSkipSet[toolName] {
		return output
	}
	// Skip if sglang was recently confirmed down (cached result only, no probe).
	if pilot.SglangRecentlyDown() {
		return output
	}

	ctx, cancel := context.WithTimeout(ctx, compressTimeout)
	defer cancel()

	prompt := fmt.Sprintf("Tool: %s\nOutput (%d chars):\n%s", toolName, len(output), output)
	if len(prompt) > 32000 {
		prompt = prompt[:32000] + "\n[... truncated]"
	}

	compressed, err := pilot.CallLocalLLM(ctx, compressSystemPrompt, prompt, compressMaxTokens)
	if err != nil {
		logger.Debug("tool output compression failed, using original", "tool", toolName, "error", err)
		return output
	}

	if len(compressed) == 0 || len(compressed) >= len(output) {
		return output
	}

	logger.Info("compressed tool output",
		"tool", toolName,
		"original", len(output),
		"compressed", len(compressed),
		"ratio", fmt.Sprintf("%.0f%%", float64(len(compressed))/float64(len(output))*100),
	)

	return fmt.Sprintf("[compressed by pilot — original %d chars]\n%s", len(output), compressed)
}

// --- 3. Auto Memory ---
// Called asynchronously after handleRunSuccess.

const (
	autoMemoryTimeout   = 90 * time.Second
	autoMemoryMaxTokens = 512
	autoMemoryMinInput  = 100 // skip for very short conversations
	autoMemoryMinOutput = 50
)

const autoMemorySystemPrompt = `You are a memory extraction assistant for a personal AI assistant.
Given a user's question and the AI's response, extract ONLY information that helps
understand the USER better for future sessions:
- User preferences and communication style (답변 스타일, 톤, 깊이 선호도)
- Personality traits, habits, values revealed through conversation
- Relationship dynamics (corrections, satisfaction, frustration, trust signals)
- Lasting decisions that constrain future work (NOT routine operations)
- Reusable solutions the user would explicitly want recalled

Do NOT store:
- Routine code changes, bug fixes, file edits, feature additions
- One-time debugging steps, transient project state
- Standard tool operations (git, npm, make, etc.)
- Implementation details of a specific task

Rules:
- If nothing reveals something about the USER, reply with just "SKIP"
- Format as bullet points starting with "- "
- Max 5 bullets
- Write in Korean
- Be very selective — prioritize understanding the person over logging the work`

// extractAutoMemory analyzes a conversation turn and returns memory-worthy notes.
// Returns empty string if nothing worth remembering.
// isToolOnlyResponse returns true if the agent response looks like pure tool
// result relay with minimal natural language — e.g., forwarding file contents
// or command output. These turns rarely contain user-model-worthy information.
func isToolOnlyResponse(response string) bool {
	trimmed := strings.TrimSpace(response)
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return true
	}
	// Count lines that look like natural language (not code/output).
	naturalLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip lines that look like code, output, or tool markup.
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "    ") ||
			strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "|") ||
			strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") {
			continue
		}
		naturalLines++
	}
	// If less than 3 lines of natural language, it's likely a tool-only response.
	return naturalLines < 3
}

func extractAutoMemory(ctx context.Context, userMessage, agentResponse string, logger *slog.Logger) string {
	if len(userMessage) < autoMemoryMinInput || len(agentResponse) < autoMemoryMinOutput {
		return ""
	}
	if isToolOnlyResponse(agentResponse) {
		return ""
	}
	// Skip if sglang was recently confirmed down (cached result only, no probe).
	if pilot.SglangRecentlyDown() {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, autoMemoryTimeout)
	defer cancel()

	prompt := fmt.Sprintf("User:\n%s\n\nAssistant:\n%s",
		pilot.TruncateInput(userMessage, 4000),
		pilot.TruncateInput(agentResponse, 8000))

	result, err := pilot.CallLocalLLM(ctx, autoMemorySystemPrompt, prompt, autoMemoryMaxTokens)
	if err != nil {
		logger.Debug("auto memory extraction failed", "error", err)
		return ""
	}

	result = strings.TrimSpace(result)
	if result == "" || result == "SKIP" || strings.ToLower(result) == "skip" {
		return ""
	}

	return result
}

// appendToMemoryFile appends extracted memories to MEMORY.md.
func appendToMemoryFile(workspaceDir, content string, logger *slog.Logger) {
	memoryPath := filepath.Join(workspaceDir, "MEMORY.md")

	// Create file with header if it doesn't exist.
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		header := "# Memory\n\nAuto-recorded learnings and decisions.\n\n"
		if err := os.WriteFile(memoryPath, []byte(header), 0o644); err != nil {
			logger.Error("failed to create MEMORY.md", "error", err)
			return
		}
	}

	// Append with timestamp.
	entry := fmt.Sprintf("\n## %s\n\n%s\n",
		time.Now().Format("2006-01-02 15:04"),
		content)

	f, err := os.OpenFile(memoryPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Error("failed to open MEMORY.md for append", "error", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		logger.Error("failed to append to MEMORY.md", "error", err)
	} else {
		logger.Info("auto-memory saved", "chars", len(content))
	}
}

// --- 4. Tool Activity Summary ---
// Called by ProgressTracker every N tool completions to generate a short Korean
// status line from accumulated thinking text.

const activitySummarySystemPrompt = `에이전트의 최근 생각 과정을 보고 지금 무엇을 하고 있는지 한국어 한 줄(30자 이내)로 요약하세요.

규칙:
- "~하는 중" 형태로 끝내기 (예: "테스트 지연 원인 파악하는 중", "수정 결과 검증하는 중")
- 따옴표, 이모지, 부가 설명 없이 요약만 출력
- 개별 동작(검색, 파일 읽기 등)이 아니라 전체 목적 관점에서 왜 그걸 하는지 요약
- 나쁜 예: "코드 검색", "파일 읽는 중" / 좋은 예: "캐시 구조 이해하는 중", "빌드 오류 원인 추적 중"`

// SummarizeToolActivity uses the local sglang model to summarize recent agent
// thinking into a short Korean phrase for the progress tracker status line.
// Returns empty string on failure (caller should treat as a no-op).
func SummarizeToolActivity(ctx context.Context, reasons []string) (string, error) {
	if !pilot.CheckSglangHealth() {
		return "", fmt.Errorf("sglang unavailable")
	}

	// Build user message from recent thinking snippets.
	var b strings.Builder
	b.WriteString("최근 에이전트 생각 과정:\n\n")
	limit := len(reasons)
	if limit > 5 {
		reasons = reasons[limit-5:]
	}
	for i, r := range reasons {
		snippet := pilot.TruncateInput(r, 300)
		fmt.Fprintf(&b, "%d. %s\n", i+1, snippet)
	}

	result, err := pilot.CallLocalLLM(ctx, activitySummarySystemPrompt, b.String(), 64)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result), nil
}
