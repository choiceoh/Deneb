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

// localai_hooks.go — local AI model hooks into the agent pipeline:
//
//  1. Tool Output Compression: after tool execution, compress large outputs
//  2. Auto Memory: after successful run, extract key learnings to MEMORY.md

// deferredSubagentNotifications wraps a subagent notification channel into a
// DeferredSystemText function. On each turn, it drains all available
// notifications and returns them joined. Returns nil if the channel is nil.
func deferredSubagentNotifications(subagentCh <-chan string) func() string {
	if subagentCh == nil {
		return nil
	}
	return func() string {
		var parts []string
		for {
			select {
			case notif := <-subagentCh:
				if notif != "" {
					parts = append(parts, notif)
				}
			default:
				return strings.Join(parts, "\n\n")
			}
		}
	}
}

// --- 2. Tool Output Compression ---
// Called in the agent loop after tool execution, before feeding results back to LLM.

const (
	compressThreshold = 16000 // chars — only compress very large outputs (saves local AI calls)
	compressMaxTokens = 1024
	compressTimeout   = 20 * time.Second
	// Tools whose output should never be compressed (they're already structured/small).
)

const compressSystemPrompt = `You are a tool output compressor.
Condense the tool output to its essential information. Preserve:
- Error messages and exit codes
- Key data points and numbers
- File paths and line numbers
- Important patterns and findings
Remove verbose boilerplate, repeated lines, and padding.
Keep the same language. Be concise but don't lose critical details.
Max 30 lines.`

// compressToolOutput shrinks a large tool output using the local AI model.
// Returns the original output if compression is not needed or fails.
func compressToolOutput(ctx context.Context, toolName, output string, logger *slog.Logger) string {
	if len(output) < compressThreshold {
		return output
	}
	if toolCompressSkipSet[toolName] {
		return output
	}
	// Skip if local AI was recently confirmed down (cached result only, no probe).
	if pilot.LocalAIRecentlyDown() {
		return output
	}

	// Concurrency is managed by the centralized local AI hub's token budget.
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
	// Skip if local AI was recently confirmed down (cached result only, no probe).
	if pilot.LocalAIRecentlyDown() {
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

const activitySummarySystemPrompt = `에이전트의 최근 도구 사용 내역을 보고 지금 무엇을 하고 있는지 한국어 한 줄(30자 이내)로 요약하세요.

규칙:
- "~하는 중" 형태로 끝내기 (예: "테스트 지연 원인 파악하는 중", "수정 결과 검증하는 중")
- 따옴표, 이모지, 부가 설명 없이 요약만 출력
- 개별 동작(검색, 파일 읽기 등)이 아니라 전체 목적 관점에서 왜 그걸 하는지 요약
- 나쁜 예: "코드 검색", "파일 읽는 중" / 좋은 예: "캐시 구조 이해하는 중", "빌드 오류 원인 추적 중"`

// SummarizeToolActivity uses the local AI model to summarize recent agent
// tool activity into a short Korean phrase for the progress tracker status line.
// The input is a slice of tool activity descriptions (e.g., "read: progress.go",
// "grep: OnToolStart in gateway-go/"). Returns empty string on failure.
func SummarizeToolActivity(ctx context.Context, activities []string) (string, error) {
	if !pilot.CheckLocalAIHealth() {
		return "", fmt.Errorf("localai unavailable")
	}

	// Concurrency is managed by the centralized local AI hub's token budget.
	// Build user message from recent tool activity descriptions.
	var b strings.Builder
	b.WriteString("최근 에이전트 도구 사용 내역:\n\n")
	limit := len(activities)
	if limit > 5 {
		activities = activities[limit-5:]
	}
	for i, a := range activities {
		snippet := pilot.TruncateInput(a, 200)
		fmt.Fprintf(&b, "%d. %s\n", i+1, snippet)
	}

	result, err := pilot.CallLocalLLM(ctx, activitySummarySystemPrompt, b.String(), 64)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result), nil
}
