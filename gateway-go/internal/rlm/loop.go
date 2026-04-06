package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
)

// LoopConfig configures a single RunLoop invocation.
type LoopConfig struct {
	Client agent.LLMStreamer // LLM client
	Model  string
	System json.RawMessage // base system prompt (loop prompt is appended)

	MaxTokens       int // max output tokens per LLM call
	MaxIter         int
	CompactThreshold int // estimated token count triggering compaction
	MaxConsecErrors int
	FallbackEnabled bool

	REPLEnv *repl.Env
	Budget  *TokenBudget
	Logger  *slog.Logger

	// Callbacks for streaming integration with the chat pipeline.
	OnTextDelta func(text string)
	OnIterStart func(iter, totalIters int)
	OnFinal     func(answer string)
}

// LoopResult holds the outcome of a RunLoop invocation.
type LoopResult struct {
	FinalAnswer    string
	Iterations     int
	TotalTokensIn  int
	TotalTokensOut int
	CompactionCount int
	ErrorCount     int
	StopReason     string // "final", "max_iterations", "max_errors", "budget", "cancelled"
	FallbackUsed   bool
}

// RunLoop executes the independent RLM iteration loop.
//
// Each iteration: call LLM (no tools) → extract code blocks from text →
// execute in Starlark REPL → check FINAL() → append results → repeat.
func RunLoop(ctx context.Context, cfg LoopConfig, userPrompt string) (*LoopResult, error) {
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = 25
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}
	if cfg.MaxConsecErrors <= 0 {
		cfg.MaxConsecErrors = 5
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	result := &LoopResult{}

	// Build the system prompt: base + loop-specific instructions.
	rlmCfg := ConfigFromEnv()
	loopPrompt := LoopSystemPrompt(rlmCfg)
	system := llm.AppendSystemText(cfg.System, loopPrompt)

	// Internal message history for the loop.
	messages := []llm.Message{
		llm.NewTextMessage("user", userPrompt),
	}

	var consecutiveErrors int

	for iter := 0; iter < cfg.MaxIter; iter++ {
		result.Iterations = iter + 1

		// Check context cancellation.
		if ctx.Err() != nil {
			result.StopReason = "cancelled"
			return result, nil
		}

		// Check token budget.
		if cfg.Budget != nil && cfg.Budget.Remaining() <= 0 {
			result.StopReason = "budget"
			break
		}

		// Callback: iteration start.
		if cfg.OnIterStart != nil {
			cfg.OnIterStart(iter, cfg.MaxIter)
		}

		// Compaction check: estimate token count and summarize if needed.
		if cfg.CompactThreshold > 0 && estimateTokens(messages) > cfg.CompactThreshold {
			compacted, err := compactHistory(ctx, cfg, system, messages)
			if err != nil {
				cfg.Logger.Warn("loop compaction failed, continuing", "error", err)
			} else {
				messages = compacted
				result.CompactionCount++
				cfg.Logger.Info("loop compaction completed",
					"iter", iter, "messages_after", len(messages))
			}
		}

		// Append iteration prompt to guide the LLM.
		iterPrompt := LoopIterationPrompt(iter, cfg.MaxIter)
		turnMessages := make([]llm.Message, len(messages))
		copy(turnMessages, messages)
		turnMessages = append(turnMessages, llm.NewTextMessage("user", iterPrompt))

		cfg.Logger.Info("loop iteration starting",
			"iter", iter, "messages", len(turnMessages))

		// Call LLM (non-streaming for simplicity; the loop owns the cycle).
		req := llm.ChatRequest{
			Model:     cfg.Model,
			Messages:  turnMessages,
			System:    system,
			MaxTokens: cfg.MaxTokens,
		}

		start := time.Now()
		responseText, err := cfg.Client.Complete(ctx, req)
		elapsed := time.Since(start)

		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = "cancelled"
				return result, nil
			}
			return nil, fmt.Errorf("loop LLM call (iter %d): %w", iter, err)
		}

		// Rough token estimate for budget tracking.
		estIn := estimateTokens(turnMessages) + estimateSystemTokens(system)
		estOut := len(responseText) / 2
		result.TotalTokensIn += estIn
		result.TotalTokensOut += estOut
		if cfg.Budget != nil {
			cfg.Budget.TryReserve(estIn + estOut)
		}

		cfg.Logger.Info("loop LLM response",
			"iter", iter, "response_len", len(responseText),
			"elapsed_ms", elapsed.Milliseconds())

		// Callback: text delta (entire response since we use Complete).
		if cfg.OnTextDelta != nil && responseText != "" {
			cfg.OnTextDelta(responseText)
		}

		// Check for FINAL() in the LLM text itself (outside code blocks).
		if answer := extractTextFinal(responseText); answer != "" {
			result.FinalAnswer = answer
			result.StopReason = "final"
			if cfg.OnFinal != nil {
				cfg.OnFinal(answer)
			}
			return result, nil
		}

		// Extract and execute code blocks.
		blocks := extractCodeBlocks(responseText)

		if len(blocks) == 0 {
			// No code blocks — append the response as-is and continue.
			messages = append(messages,
				llm.NewTextMessage("assistant", responseText),
			)
			continue
		}

		// Execute each code block in the REPL.
		var execOutputs []string
		iterHadError := false

		for _, block := range blocks {
			cfg.REPLEnv.ResetFinal()
			execResult := cfg.REPLEnv.Execute(block)

			var output strings.Builder
			if execResult.Stdout != "" {
				output.WriteString(execResult.Stdout)
			}
			if execResult.Error != "" {
				iterHadError = true
				result.ErrorCount++
				if output.Len() > 0 {
					output.WriteByte('\n')
				}
				output.WriteString("Error: ")
				output.WriteString(execResult.Error)
			}
			execOutputs = append(execOutputs, output.String())

			// Check FINAL() from REPL execution.
			if cfg.REPLEnv.HasFinal() {
				result.FinalAnswer = cfg.REPLEnv.FinalAnswer()
				result.StopReason = "final"
				if cfg.OnFinal != nil {
					cfg.OnFinal(result.FinalAnswer)
				}
				cfg.Logger.Info("loop FINAL detected",
					"iter", iter, "answer_len", len(result.FinalAnswer))
				return result, nil
			}
		}

		// Track consecutive errors.
		if iterHadError {
			consecutiveErrors++
			if consecutiveErrors >= cfg.MaxConsecErrors {
				result.StopReason = "max_errors"
				cfg.Logger.Warn("loop max consecutive errors reached",
					"errors", consecutiveErrors)
				break
			}
		} else {
			consecutiveErrors = 0
		}

		// Append assistant response + execution results to history.
		messages = append(messages,
			llm.NewTextMessage("assistant", responseText),
			llm.NewTextMessage("user", formatExecResults(execOutputs)),
		)
	}

	// Iterations exhausted or error limit reached — try fallback.
	if result.StopReason == "" {
		result.StopReason = "max_iterations"
	}

	if cfg.FallbackEnabled && result.FinalAnswer == "" {
		cfg.Logger.Info("loop generating fallback answer",
			"stop_reason", result.StopReason)
		fallback, err := generateFallback(ctx, cfg, system, messages)
		if err != nil {
			cfg.Logger.Warn("loop fallback generation failed", "error", err)
		} else {
			result.FinalAnswer = fallback
			result.FallbackUsed = true
		}
	}

	return result, nil
}

// ── Code block extraction ───────────────────────────────────────────────────

// codeBlockRe matches fenced code blocks with optional language tags.
var codeBlockRe = regexp.MustCompile("(?s)```(?:starlark|python|star|repl)?\\s*\n(.*?)```")

// extractCodeBlocks returns the code contents of all fenced code blocks.
func extractCodeBlocks(text string) []string {
	matches := codeBlockRe.FindAllStringSubmatch(text, -1)
	blocks := make([]string, 0, len(matches))
	for _, m := range matches {
		code := strings.TrimSpace(m[1])
		if code != "" {
			blocks = append(blocks, code)
		}
	}
	return blocks
}

// textFinalRe matches FINAL("answer") or FINAL('answer') outside code blocks.
var textFinalRe = regexp.MustCompile(`(?s)FINAL\s*\(\s*["'](.+?)["']\s*\)`)

// extractTextFinal finds a FINAL("...") call in plain text (outside code blocks).
// Returns empty string if not found.
func extractTextFinal(text string) string {
	// Strip code blocks first so we don't match FINAL inside code.
	stripped := codeBlockRe.ReplaceAllString(text, "")
	m := textFinalRe.FindStringSubmatch(stripped)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// formatExecResults formats execution outputs for the user message.
func formatExecResults(outputs []string) string {
	if len(outputs) == 1 {
		return "실행 결과:\n" + outputs[0]
	}
	var b strings.Builder
	for i, out := range outputs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "블록 %d 결과:\n%s", i+1, out)
	}
	return b.String()
}

// estimateTokens provides a rough token estimate for messages (runes/2).
func estimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 2
	}
	return total
}

// estimateSystemTokens estimates token count for the system prompt.
func estimateSystemTokens(system json.RawMessage) int {
	return len(system) / 2
}

// compactHistory summarizes older messages to reduce context size.
func compactHistory(ctx context.Context, cfg LoopConfig, system json.RawMessage, messages []llm.Message) ([]llm.Message, error) {
	if len(messages) <= 4 {
		return messages, nil // too short to compact
	}

	// Keep first message (original user prompt) + last 4 messages.
	const keepTail = 4
	head := messages[:1]
	tail := messages[len(messages)-keepTail:]
	middle := messages[1 : len(messages)-keepTail]

	// Summarize the middle via a single LLM call.
	summaryMsgs := make([]llm.Message, 0, len(middle)+1)
	summaryMsgs = append(summaryMsgs, middle...)
	summaryMsgs = append(summaryMsgs, llm.NewTextMessage("user", LoopCompactionPrompt()))

	req := llm.ChatRequest{
		Model:     cfg.Model,
		Messages:  summaryMsgs,
		System:    system,
		MaxTokens: 2048,
	}

	summary, err := cfg.Client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("compaction LLM call: %w", err)
	}

	// Reassemble: original prompt + summary + recent messages.
	compacted := make([]llm.Message, 0, 2+keepTail)
	compacted = append(compacted, head...)
	compacted = append(compacted, llm.NewTextMessage("assistant",
		fmt.Sprintf("[이전 %d회 반복 요약]\n%s", len(middle)/2, summary)))
	compacted = append(compacted, tail...)
	return compacted, nil
}

// generateFallback makes one final LLM call to produce a best-effort answer.
func generateFallback(ctx context.Context, cfg LoopConfig, system json.RawMessage, messages []llm.Message) (string, error) {
	fallbackMsgs := make([]llm.Message, len(messages))
	copy(fallbackMsgs, messages)
	fallbackMsgs = append(fallbackMsgs, llm.NewTextMessage("user", LoopFallbackPrompt()))

	req := llm.ChatRequest{
		Model:     cfg.Model,
		Messages:  fallbackMsgs,
		System:    system,
		MaxTokens: 4096,
	}

	return cfg.Client.Complete(ctx, req)
}
