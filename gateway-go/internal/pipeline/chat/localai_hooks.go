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
	if _, ok := toolCompressSkipSet[toolName]; ok {
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
		prompt = textutil.TruncateBytes(prompt, 32000) + "\n[... truncated]"
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

	return fmt.Sprintf("[compressed by pilot — original %d chars]\n%s", len(output), compressed)
}
