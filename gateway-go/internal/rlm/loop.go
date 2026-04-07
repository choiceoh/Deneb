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

	MaxTokens        int // max output tokens per LLM call
	MaxIter          int
	CompactThreshold int // explicit override; 0 = use percentage from Config
	MaxConsecErrors  int
	FallbackEnabled  bool

	REPLEnv    *repl.Env
	Budget     *TokenBudget
	Logger     *slog.Logger
	TraceStore *TraceStore // optional; when set, a Trace is recorded and stored

	// Callbacks for streaming integration with the chat pipeline.
	OnTextDelta func(text string)
	OnIterStart func(iter, totalIters int)
	OnFinal     func(answer string)
}

// LoopResult holds the outcome of a RunLoop invocation.
type LoopResult struct {
	FinalAnswer     string
	Iterations      int
	TotalTokensIn   int
	TotalTokensOut  int
	CompactionCount int
	ErrorCount      int
	StopReason      string // "final", "max_iterations", "max_errors", "budget", "cancelled"
	FallbackUsed    bool
}

// RunLoop executes the independent RLM iteration loop.
//
// Each iteration: call LLM (no tools) → extract code blocks from text →
// execute in Starlark REPL → check FINAL() → append results → repeat.
func RunLoop(ctx context.Context, cfg LoopConfig, userPrompt string) (*LoopResult, error) {
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = 30
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 16384
	}
	if cfg.MaxConsecErrors <= 0 {
		cfg.MaxConsecErrors = 5
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	result := &LoopResult{}
	loopStart := time.Now()

	// Build the system prompt: base + loop-specific instructions.
	rlmCfg := ConfigFromEnv()
	loopPrompt := LoopSystemPrompt(rlmCfg)
	system := llm.AppendSystemText(cfg.System, loopPrompt)

	// Compute compaction threshold: explicit override or percentage of model context.
	compactThreshold := cfg.CompactThreshold
	if compactThreshold <= 0 {
		compactThreshold = int(float64(rlmCfg.ModelContextLimit) * rlmCfg.CompactionThresholdPct)
	}

	// Trace: prepare trace if store is available.
	tracing := cfg.TraceStore != nil
	traceID := fmt.Sprintf("rlm-%d", loopStart.UnixMilli())
	var traceSteps []IterationTrace

	// Internal message history for the loop.
	messages := []llm.Message{
		llm.NewTextMessage("user", userPrompt),
	}

	var consecutiveErrors int

	// finishTrace assembles and stores the trace from accumulated data.
	finishTrace := func() {
		if !tracing {
			return
		}
		now := time.Now()
		t := Trace{
			ID:          traceID,
			StartedAt:   loopStart,
			FinishedAt:  now,
			ElapsedMS:   now.Sub(loopStart).Milliseconds(),
			UserPrompt:  truncate(userPrompt, 500),
			Model:       cfg.Model,
			StopReason:  result.StopReason,
			Iterations:  result.Iterations,
			TotalIn:     result.TotalTokensIn,
			TotalOut:    result.TotalTokensOut,
			Compactions: result.CompactionCount,
			Errors:      result.ErrorCount,
			FinalLen:    len(result.FinalAnswer),
			Steps:       traceSteps,
		}
		cfg.TraceStore.Add(t)
	}

	for iter := 0; iter < cfg.MaxIter; iter++ {
		iterStart := time.Now()
		result.Iterations = iter + 1

		// Check context cancellation.
		if ctx.Err() != nil {
			result.StopReason = "cancelled"
			finishTrace()
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

		// Per-iteration trace data.
		step := IterationTrace{
			Iter:      iter,
			StartedAt: iterStart,
		}

		// Compaction check: estimate token count and summarize if needed.
		if compactThreshold > 0 && estimateTokens(messages) > compactThreshold {
			compacted, err := compactHistory(ctx, cfg, system, messages)
			if err != nil {
				cfg.Logger.Warn("loop compaction failed, continuing", "error", err)
			} else {
				messages = compacted
				result.CompactionCount++
				step.Compacted = true
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

		llmStart := time.Now()
		responseText, err := cfg.Client.Complete(ctx, req)
		llmElapsed := time.Since(llmStart)

		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = "cancelled"
				finishTrace()
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

		step.LLMElapsed = llmElapsed.Milliseconds()
		step.ResponseLen = len(responseText)
		step.TokensIn = estIn
		step.TokensOut = estOut

		cfg.Logger.Info("loop LLM response",
			"iter", iter, "response_len", len(responseText),
			"elapsed_ms", llmElapsed.Milliseconds())

		// Callback: text delta (entire response since we use Complete).
		if cfg.OnTextDelta != nil && responseText != "" {
			cfg.OnTextDelta(responseText)
		}

		// Check for FINAL() in the LLM text itself (outside code blocks).
		if answer := extractTextFinal(responseText); answer != "" {
			if isPrematureFinal(answer, iter) {
				cfg.Logger.Warn("premature text FINAL rejected, continuing loop",
					"iter", iter, "answer_len", len(answer))
				// Fall through — treat as normal text response.
			} else {
				result.FinalAnswer = answer
				result.StopReason = "final"
				step.HasFinal = true
				step.TotalElapsed = time.Since(iterStart).Milliseconds()
				if tracing {
					traceSteps = append(traceSteps, step)
				}
				if cfg.OnFinal != nil {
					cfg.OnFinal(answer)
				}
				finishTrace()
				return result, nil
			}
		}

		// Extract and execute code blocks.
		blocks := extractCodeBlocks(responseText)
		step.CodeBlocks = len(blocks)

		if len(blocks) == 0 {
			// No code blocks — append the response as-is and continue.
			step.TotalElapsed = time.Since(iterStart).Milliseconds()
			if tracing {
				traceSteps = append(traceSteps, step)
			}
			messages = append(messages,
				llm.NewTextMessage("assistant", responseText),
			)
			continue
		}

		// Record code snippets for trace.
		if tracing {
			for _, block := range blocks {
				step.CodeSnippets = append(step.CodeSnippets, truncate(block, 200))
			}
		}

		// Execute each code block in the REPL.
		var execOutputs []string
		iterHadError := false
		execStart := time.Now()

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

				if tracing {
					step.ExecErrors = append(step.ExecErrors, truncate(execResult.Error, 300))
				}
			}
			execOutputs = append(execOutputs, output.String())

			if tracing {
				step.ExecOutputs = append(step.ExecOutputs, truncate(output.String(), 500))
			}

			// Check FINAL() from REPL execution.
			if cfg.REPLEnv.HasFinal() {
				finalAnswer := cfg.REPLEnv.FinalAnswer()
				if isPrematureFinal(finalAnswer, iter) {
					cfg.Logger.Warn("premature REPL FINAL rejected, continuing loop",
						"iter", iter, "answer_len", len(finalAnswer))
					cfg.REPLEnv.ResetFinal()
				} else {
					result.FinalAnswer = finalAnswer
					result.StopReason = "final"
					step.HasFinal = true
					step.HasError = iterHadError
					step.ExecElapsed = time.Since(execStart).Milliseconds()
					step.TotalElapsed = time.Since(iterStart).Milliseconds()
					if tracing {
						traceSteps = append(traceSteps, step)
					}
					if cfg.OnFinal != nil {
						cfg.OnFinal(result.FinalAnswer)
					}
					cfg.Logger.Info("loop FINAL detected",
						"iter", iter, "answer_len", len(result.FinalAnswer))
					finishTrace()
					return result, nil
				}
			}
		}

		step.HasError = iterHadError
		step.ExecElapsed = time.Since(execStart).Milliseconds()
		step.TotalElapsed = time.Since(iterStart).Milliseconds()
		if tracing {
			traceSteps = append(traceSteps, step)
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

	finishTrace()
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
// Uses proper quote-matching to avoid mismatches with embedded quotes
// (e.g. FINAL("he said 'hello'") won't stop at the inner single quote).
// (?s) enables dot-matches-newline for multiline answers.
var textFinalRe = regexp.MustCompile(`(?s)FINAL\s*\(\s*(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)')\s*\)`)

// extractTextFinal finds a FINAL("...") call in plain text (outside code blocks).
// Returns empty string if not found.
func extractTextFinal(text string) string {
	// Strip code blocks first so we don't match FINAL inside code.
	stripped := codeBlockRe.ReplaceAllString(text, "")
	m := textFinalRe.FindStringSubmatch(stripped)
	if m == nil {
		return ""
	}
	// m[1] is the double-quote capture group, m[2] is the single-quote group.
	if m[1] != "" {
		return m[1]
	}
	return m[2]
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

// estimateTokens provides a rough token estimate for messages.
// Uses bytes/4 which better approximates token count for Korean-heavy content
// (Korean UTF-8: 3 bytes per char ≈ 0.7 tokens; English: 1 byte ≈ 0.25 tokens).
func estimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
	}
	return total
}

// estimateSystemTokens estimates token count for the system prompt.
func estimateSystemTokens(system json.RawMessage) int {
	return len(system) / 4
}

// ── Premature FINAL guard ──────────────────────────────────────────────────
//
// Addresses the "brittle FINAL detection" issue from the RLM paper
// (Zhang et al., 2025, Appendix A): models sometimes wrap their plan
// or intention in FINAL() instead of the actual computed answer.

// isPrematureFinal returns true when a FINAL answer is likely a plan or
// thought rather than a genuine answer. Rejected FINALs cause the loop
// to continue iterating so the model can actually compute.
func isPrematureFinal(answer string, iter int) bool {
	// After sufficient exploration, trust the model's judgment.
	if iter >= 2 {
		return false
	}

	trimmed := strings.TrimSpace(answer)

	// Question-form answers on early iterations are suspicious —
	// the model is asking itself what to do, not answering.
	if strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？") {
		return true
	}

	// Plan/intention language: 2+ hits means the model is describing
	// its strategy rather than providing a computed answer.
	planHits := 0
	for _, p := range prematurePlanIndicators {
		if strings.Contains(answer, p) {
			planHits++
			if planHits >= 2 {
				return true
			}
		}
	}

	return false
}

// prematurePlanIndicators are phrases that signal the model is describing
// what it intends to do rather than providing a computed answer.
var prematurePlanIndicators = []string{
	// Korean future-intent verb endings
	"하겠습니다", "살펴보겠", "분석하겠", "확인하겠",
	"탐색하겠", "진행하겠", "시작하겠", "알아보겠", "찾아보겠",
	// Korean planning nouns
	"단계별로", "계획은", "전략은",
	// English intent patterns
	"I will ", "I'll ", "Let me ", "My plan",
	"I need to ", "I should ", "Step 1:", "First, I",
}

// compactHistory summarizes older messages to reduce context size.
func compactHistory(ctx context.Context, cfg LoopConfig, system json.RawMessage, messages []llm.Message) ([]llm.Message, error) {
	if len(messages) <= 4 {
		return messages, nil // too short to compact
	}

	// Keep first message (original user prompt) + last 10 messages (5 turns).
	const keepTail = 10
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
		MaxTokens: 4096,
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
