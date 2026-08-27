package chat

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// localai_hooks.go — local AI model hooks into the agent pipeline:
//
//  1. Proactive Context: before agent run, scan related files/memory to enrich system prompt
//  2. Tool Output Compression: after tool execution, compress large outputs

// deferredSubagentNotifications wraps a subagent notification channel into a
// DeferredTurnNotices function: each poll drains all available notifications.
// The executor appends them as text blocks on the next tool-results user
// message — NOT the system prompt, whose mid-run growth invalidated the
// (system, tools) prefix for content-prefix provider caches (kimi) on every
// late notification. Undrained notices stay in the buffered channel (it is
// session-scoped) and surface on the next run. Returns nil if the channel is
// nil.
func deferredSubagentNotifications(subagentCh <-chan string) func() []string {
	if subagentCh == nil {
		return nil
	}
	return func() []string {
		var parts []string
		for {
			select {
			case notif := <-subagentCh:
				if notif != "" {
					parts = append(parts, notif)
				}
			default:
				return parts
			}
		}
	}
}

// --- 2. Tool Output Compression ---
// Called in the agent loop after tool execution, before feeding results back to LLM.

const (
	compressThreshold = 16000 // bytes — only compress very large outputs (saves local AI calls)
	compressMaxTokens = 1024
	compressTimeout   = 20 * time.Second
	// compressPromptMaxBytes caps what the compressor SEES. Beyond it the
	// summary covers only the head of the output, which the result marker
	// declares (partialCoverageNote) so the model does not mistake a partial
	// summary for a complete one.
	compressPromptMaxBytes = 32000
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
//
// When the caller ASKED for compression and the harness declined for a reason
// the caller cannot see — this tool is on the skip list, or the local
// summarizer is down — the original comes back with a one-line notice. The
// system prompt advertises `compress: true` for "any tool input", so a silent
// decline reads as "compressed" and the caller keeps a full-size result it
// believes is small (observed 2026-08-26: wiki/grep/blackboard and four more
// are on the skip list). A benign decline (output below the threshold, or a
// summary that saved nothing) stays silent: nothing was lost there.
func compressToolOutput(ctx context.Context, toolName, output, spillID string, logger *slog.Logger) string {
	if len(output) < compressThreshold {
		return output
	}
	if _, ok := toolCompressSkipSet[toolName]; ok {
		return output + compressDeclinedNotice(toolName+" 도구는 압축 대상이 아니다", spillID)
	}
	// Skip if local AI was recently confirmed down (cached result only, no probe).
	if pilot.LocalAIRecentlyDown() {
		return output + compressDeclinedNotice("로컬 요약 모델이 응답하지 않는다", spillID)
	}

	// Concurrency is managed by the centralized local AI hub's token budget.
	ctx, cancel := context.WithTimeout(ctx, compressTimeout)
	defer cancel()

	prompt := fmt.Sprintf("Tool: %s\nOutput (%d chars):\n%s", toolName, len(output), output)
	// The compressor sees at most this much. On a large output that is a small
	// FRACTION of it, and the result marker below must say so — otherwise the
	// model reads "compressed — original 204800 chars" over a summary that only
	// covers the first 32KB and treats it as a summary of the whole.
	partial := len(prompt) > compressPromptMaxBytes
	if partial {
		prompt = textutil.TruncateBytes(prompt, compressPromptMaxBytes) + "\n[... truncated]"
	}

	// Tiny role (2026-07-21 helper eval): bounded 30-line compression is easy
	// tier, fail-open (original kept on any failure), and this runs INSIDE the
	// turn — the 2.5x latency win lands directly on turn feel.
	compressed, err := pilot.CallTinyLLM(ctx, compressSystemPrompt, prompt, compressMaxTokens)
	if err != nil {
		logger.Debug("tool output compression failed, using original", "tool", toolName, "error", err)
		return output
	}

	if compressed == "" || len(compressed) >= len(output) {
		return output
	}

	logger.Info(
		"compressed tool output",
		"tool", toolName,
		"original", len(output),
		"compressed", len(compressed),
		"ratio", fmt.Sprintf("%.0f%%", float64(len(compressed))/float64(len(output))*100),
	)

	if spillID != "" {
		return sprintfCompressed(output, spillID, compressed, partial)
	}
	return sprintfCompressedPlain(output, compressed, partial)
}

// sprintfCompressed renders the compressed result with the spill handle so the
// model can recover the full output; sprintfCompressedPlain is the no-handle
// form (nothing was stored).
func sprintfCompressed(original, spillID, compressed string, partial bool) string {
	return fmt.Sprintf("[compressed by pilot — original %d bytes%s; 원문: read_spillover(spill_id=%q)]\n%s",
		len(original), partialCoverageNote(partial), spillID, compressed)
}

func sprintfCompressedPlain(original, compressed string, partial bool) string {
	return fmt.Sprintf("[compressed by pilot — original %d bytes%s]\n%s",
		len(original), partialCoverageNote(partial), compressed)
}

// partialCoverageNote states that the summary covers only the head of the
// output. Silence here let a 16%-coverage summary read as a complete one.
func partialCoverageNote(partial bool) string {
	if !partial {
		return ""
	}
	return fmt.Sprintf(", 앞 %dKB만 요약됨 — 나머지가 필요하면 원문을 읽어라", compressPromptMaxBytes/1000)
}

// compressDeclinedNotice renders the one-line notice appended when a requested
// compression did not happen. The spill handle rides along when one exists, so
// the caller can still shrink its own context by reading a slice later.
func compressDeclinedNotice(reason, spillID string) string {
	if spillID != "" {
		return fmt.Sprintf("\n\n[compress 요청됨 · 미적용: %s — 원문 그대로다. 필요하면 read_spillover(spill_id=%q)로 필요한 부분만 읽어라.]", reason, spillID)
	}
	return fmt.Sprintf("\n\n[compress 요청됨 · 미적용: %s — 원문 그대로다.]", reason)
}
