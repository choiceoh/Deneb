// executor.go — Core agent execution loop.
//
// RunAgent implements the LLM → tool-call → repeat cycle shared by both the
// chat pipeline (chat/) and the auto-reply pipeline (autoreply/).  All
// LLM-update surface area (thinking budget, tool streaming, content block
// layout) lives here; callers only need to write thin adapters that map their
// domain-specific config to AgentConfig.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

// RunAgent executes the agent tool-call loop: call LLM → detect tool_use →
// execute tool → feed result → repeat until the model stops or limits are hit.
//
// client must satisfy LLMStreamer (*llm.Client does).
// tools may be nil if no tool use is expected.
// hooks provides optional callbacks for streaming events; pass zero-value if not needed.
// runLog may be nil; if provided it records per-turn LLM and tool events.
func RunAgent(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client LLMStreamer,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*AgentResult, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 25
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	result := &AgentResult{}

	// Max-output-tokens recovery: tracks how many times we've auto-resumed
	// after the LLM response was truncated by max_tokens.
	var maxTokensRecoveryCount int
	baseMaxTokens := cfg.MaxTokens // Original value before any recovery scaling.

	for turn := range cfg.MaxTurns {
		result.Turns = turn + 1

		// Per-turn context initialization (e.g., injecting a TurnContext for
		// cross-tool result sharing). The caller sets cfg.OnTurnInit to provide
		// this behaviour; the executor itself stays context-agnostic.
		if cfg.OnTurnInit != nil {
			ctx = cfg.OnTurnInit(ctx)
		}

		// Deferred system text injection: from turn 1 onward, check if
		// late-arriving context (e.g., proactive hints, subagent completion
		// notifications) is ready. The hook is kept alive so multiple sources
		// can deliver text across different turns (e.g., proactive hint on
		// turn 1, subagent notification on turn 5).
		if turn > 0 && cfg.DeferredSystemText != nil {
			if extra := cfg.DeferredSystemText(); extra != "" {
				cfg.System = llm.AppendSystemText(cfg.System, extra)
			}
		}

		// Dynamic tool injection: from turn 1 onward, check if new tools
		// were activated (e.g., via fetch_tools). Append them to cfg.Tools
		// so they appear in subsequent LLM requests.
		if turn > 0 && cfg.DynamicToolsProvider != nil {
			if extra := cfg.DynamicToolsProvider(); len(extra) > 0 {
				cfg.Tools = appendUniqueTools(cfg.Tools, extra)
			}
		}

		req := llm.ChatRequest{
			Model:            cfg.Model,
			Messages:         messages,
			System:           cfg.System,
			MaxTokens:        cfg.MaxTokens,
			Tools:            cfg.Tools,
			Stream:           true,
			Thinking:         cfg.Thinking,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			FrequencyPenalty: cfg.FrequencyPenalty,
			PresencePenalty:  cfg.PresencePenalty,
			StopSequences:    cfg.StopSequences,
			ResponseFormat:   cfg.ResponseFormat,
			ToolChoice:       cfg.ToolChoice,
		}

		events, err := client.StreamChat(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				result.FinalMessages = messages
				return result, nil
			}
			return nil, fmt.Errorf("stream chat (turn %d): %w", turn, err)
		}

		turnRes := &turnResult{}

		// Consume the stream for this turn. On idle stall, retry once —
		// the LLM API sometimes stalls transiently but recovers on reconnect.
		err = consumeStreamInto(ctx, events, hooks, turnRes, cfg.StreamIdleTimeout, logger)
		if errors.Is(err, ErrStreamIdle) && ctx.Err() == nil {
			logger.Warn("stream idle stall detected, retrying turn",
				"turn", turn,
				"idleTimeout", cfg.StreamIdleTimeout)
			turnRes = &turnResult{}
			events, err = client.StreamChat(ctx, req)
			if err == nil {
				err = consumeStreamInto(ctx, events, hooks, turnRes, cfg.StreamIdleTimeout, logger)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				result.FinalMessages = messages
				return result, nil
			}
			return nil, fmt.Errorf("consume stream (turn %d): %w", turn, err)
		}

		// Accumulate usage.
		result.Usage.InputTokens += turnRes.usage.InputTokens
		result.Usage.OutputTokens += turnRes.usage.OutputTokens

		// Per-turn token logging: surface per-turn cost so multi-turn runs
		// are transparent (the accumulated total can be misleading).
		logger.Info("agent turn complete",
			"turn", turn,
			"turnInputTokens", turnRes.usage.InputTokens,
			"turnOutputTokens", turnRes.usage.OutputTokens,
			"accInputTokens", result.Usage.InputTokens,
			"messages", len(messages))

		// Feed actual token usage back to the estimator for self-calibration.
		if turnRes.usage.InputTokens > 0 {
			est := tokenest.ForModel(cfg.Model)
			estimated := est.CountBytes([]byte(req.System))
			for _, m := range req.Messages {
				estimated += est.CountBytes([]byte(m.Content))
			}
			tokenest.RecordFeedback(est.Family(), estimated, turnRes.usage.InputTokens)
		}

		// Log LLM turn result to agent detail log.
		if runLog != nil {
			runLog.LogTurnLLM(agentlog.TurnLLMData{
				Turn:         turn + 1,
				InputTokens:  turnRes.usage.InputTokens,
				OutputTokens: turnRes.usage.OutputTokens,
				StopReason:   turnRes.stopReason,
				TextLen:      len(turnRes.text),
				ToolCalls:    len(turnRes.toolCalls),
			})
		}

		// Mid-run hook: notify caller of token accumulation.
		if cfg.OnTurn != nil {
			cfg.OnTurn(turn+1, result.Usage.InputTokens+result.Usage.OutputTokens)
		}

		// Text: last turn's text for channel reply (avoids re-sending
		// content already streamed to the user).
		// AllText: accumulated text from all turns for transcript persistence,
		// so intermediate findings (e.g., "tab indentation is the issue")
		// survive into the next run's context assembly.
		if turnRes.text != "" {
			result.Text = turnRes.text
			if result.AllText != "" {
				result.AllText += "\n\n"
			}
			result.AllText += turnRes.text
		}

		// --- Max-output-tokens recovery ---
		// When the LLM response is truncated by max_tokens (not a clean end_turn),
		// inject a "resume" message and retry. This prevents losing partially
		// generated code or explanations.
		if turnRes.stopReason == "max_tokens" && len(turnRes.toolCalls) == 0 &&
			cfg.MaxOutputTokensRecovery > 0 && maxTokensRecoveryCount < cfg.MaxOutputTokensRecovery {
			maxTokensRecoveryCount++

			// Scale up MaxTokens for the next call so the model has more room.
			scale := 2.0 // Default: double the original.
			if idx := maxTokensRecoveryCount - 1; idx < len(cfg.MaxOutputTokensScaleFactors) {
				scale = cfg.MaxOutputTokensScaleFactors[idx]
			}
			cfg.MaxTokens = int(float64(baseMaxTokens) * scale)

			logger.Info("max_tokens recovery: scaling output tokens and injecting resume",
				"attempt", maxTokensRecoveryCount,
				"maxAttempts", cfg.MaxOutputTokensRecovery,
				"baseMaxTokens", baseMaxTokens,
				"newMaxTokens", cfg.MaxTokens)
			// Append the truncated assistant output so the LLM sees what it already wrote.
			messages = append(messages, llm.NewBlockMessage("assistant", turnRes.contentBlocks))
			// Inject a user-role resume prompt.
			messages = append(messages, llm.NewTextMessage("user",
				"[Output was truncated due to token limit. Resume directly from where you left off — no apology, no recap.]"))
			continue
		}

		// --- Check stop reason ---
		if turnRes.stopReason == "end_turn" || len(turnRes.toolCalls) == 0 {
			// Persist the terminal assistant message (not appended to messages
			// since the loop is ending, but must be in transcript for next run).
			if cfg.OnMessagePersist != nil && turnRes.text != "" {
				cfg.OnMessagePersist(llm.NewBlockMessage("assistant", turnRes.contentBlocks))
				result.TurnsPersisted++
			}

			result.StopReason = turnRes.stopReason
			if result.StopReason == "" {
				result.StopReason = "end_turn"
			}
			result.MaxTokensRecoveries = maxTokensRecoveryCount
			result.FinalMessages = messages
			return result, nil
		}

		// After turn 0 completes and more turns follow, strip base64 image data from
		// the message history to avoid retransmitting image bytes on every subsequent
		// turn. The image was already consumed by the model on turn 0; subsequent turns
		// only need the text context. Each image block (~1600 tokens) becomes a tiny
		// text placeholder instead.
		if turn == 0 && cfg.StripImagesAfterFirstTurn {
			messages = stripBase64ImagesFromHistory(messages)
		}

		// Record where the current turn's messages begin in the array.
		// Everything before this index is from prior turns and eligible for
		// tool result compaction.
		currentTurnStart := len(messages)

		// Build assistant message with all content blocks from this turn.
		assistantMsg := llm.NewBlockMessage("assistant", turnRes.contentBlocks)
		messages = append(messages, assistantMsg)
		if cfg.OnMessagePersist != nil {
			cfg.OnMessagePersist(assistantMsg)
			result.TurnsPersisted++
		}

		// Execute tools sequentially in the order the LLM emitted them.
		// Parallel tool execution has been removed — tools always run one at
		// a time so cross-tool side effects are predictable.
		var toolResults []llm.ContentBlock
		if len(turnRes.toolCalls) > 0 {
			turnReason := extractThinkingText(turnRes.contentBlocks)
			toolResults = make([]llm.ContentBlock, len(turnRes.toolCalls))
			for i, tc := range turnRes.toolCalls {
				if ctx.Err() != nil {
					break
				}
				toolResults[i] = executeOneTool(ctx, tc, tools, hooks, turnReason, turn, logger, runLog, cfg.ToolLoopDetector)
			}

			// Check context cancellation after tool execution.
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				for _, tc := range turnRes.toolCalls {
					result.InterruptedToolNames = append(result.InterruptedToolNames, tc.Name)
				}
				result.FinalMessages = messages
				return result, nil
			}
		}

		// Record tool activities for context persistence.
		for i, tc := range turnRes.toolCalls {
			isErr := i < len(toolResults) && toolResults[i].IsError
			result.ToolActivities = append(result.ToolActivities, ToolActivity{
				Name:    tc.Name,
				IsError: isErr,
			})
		}

		// When the turn budget is almost spent and sub-agents are running, nudge
		// the agent to wrap up and yield to the notification system.
		spawnActive := cfg.SpawnDetected != nil && cfg.SpawnDetected()
		if spawnActive {
			if warning := buildTurnBudgetWarning(turn, cfg.MaxTurns); warning != "" {
				toolResults = append(toolResults, llm.ContentBlock{
					Type: "text",
					Text: warning,
				})
			}
		}

		toolResultMsg := llm.NewBlockMessage("user", toolResults)
		messages = append(messages, toolResultMsg)
		if cfg.OnMessagePersist != nil {
			cfg.OnMessagePersist(toolResultMsg)
			result.TurnsPersisted++
		}

		// Prior-turn tool result compaction: shrink tool_result content from
		// completed turns to CompactedMaxOutput (4K chars). The LLM already
		// saw the full result on the turn it was produced; subsequent turns
		// only need a summary. This prevents multi-turn token explosion where
		// resending full tool results (32K each) on every turn compounds cost.
		if n := CompactPriorToolResults(messages, currentTurnStart); n > 0 {
			logger.Info("compacted prior tool results",
				"turn", turn,
				"blocksCompacted", n)
		}
	}

	result.StopReason = "max_turns"
	result.MaxTokensRecoveries = maxTokensRecoveryCount
	result.FinalMessages = messages
	return result, nil
}

// buildTurnBudgetWarning returns a warning message when the agent is
// approaching the turn limit while sub-agents are running. Used to tell the
// agent to wrap up and yield to the notification system. Returns "" when no
// warning is needed.
func buildTurnBudgetWarning(currentTurn, maxTurns int) string {
	remaining := maxTurns - currentTurn - 1 // -1 because turn is 0-based and we just finished it
	if remaining <= 0 {
		return ""
	}
	// Warning at 80% of budget (5 turns remaining out of 25).
	threshold := maxTurns / 5
	if threshold < 3 {
		threshold = 3
	}
	if remaining > threshold {
		return ""
	}
	return fmt.Sprintf("[System: 턴 예산 정보 — 남은 턴 %d/%d. 서브에이전트가 작업 중입니다. 추가 작업이 없으면 턴을 종료하세요.]",
		remaining, maxTurns)
}

// appendUniqueTools appends extra tools to base, skipping any whose name
// already exists in base. Used for dynamic tool injection (deferred tools).
func appendUniqueTools(base, extra []llm.Tool) []llm.Tool {
	existing := make(map[string]struct{}, len(base))
	for _, t := range base {
		existing[t.Name] = struct{}{}
	}
	for _, t := range extra {
		if _, ok := existing[t.Name]; !ok {
			base = append(base, t)
			existing[t.Name] = struct{}{}
		}
	}
	return base
}

// turnResult holds the parsed output of a single LLM turn.
type turnResult struct {
	text          string
	stopReason    string
	toolCalls     []llm.ContentBlock
	contentBlocks []llm.ContentBlock
	usage         llm.TokenUsage
}

// defaultStreamIdleTimeout is the default maximum wait for the next SSE event
// during LLM streaming. Matches Claude Code's CLAUDE_STREAM_IDLE_TIMEOUT_MS.
const defaultStreamIdleTimeout = 90 * time.Second

// ErrStreamIdle is returned when the LLM stream stalls (no event within the
// idle timeout). The error is considered retryable by callers.
var ErrStreamIdle = fmt.Errorf("stream stalled: no event within idle timeout")

// consumeStreamInto reads all events from a streaming LLM response and
// populates the provided turnResult.
//
// idleTimeout controls how long to wait for the next event before declaring
// the stream stalled. Zero uses defaultStreamIdleTimeout; negative disables.
func consumeStreamInto(ctx context.Context, events <-chan llm.StreamEvent, hooks StreamHooks, result *turnResult, idleTimeout time.Duration, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	// Resolve idle timeout.
	if idleTimeout == 0 {
		idleTimeout = defaultStreamIdleTimeout
	}

	// Track current content block being built.
	type blockBuilder struct {
		block   llm.ContentBlock
		jsonBuf []byte // accumulator for input_json_delta
	}
	var currentBlock *blockBuilder
	var blockIndex = -1

	// Idle watchdog: detects LLM stream stalls where the TCP connection stays
	// alive but no SSE events arrive. Without this, stalled streams hang
	// indefinitely (HTTP-level timeouts are too coarse at 5+ minutes).
	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	if idleTimeout > 0 {
		idleTimer = time.NewTimer(idleTimeout)
		defer idleTimer.Stop()
		idleCh = idleTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idleCh:
			return ErrStreamIdle
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			// Reset idle watchdog on every received event.
			if idleTimer != nil {
				if !idleTimer.Stop() {
					// Drain channel if timer already fired (race window).
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(idleTimeout)
			}

			switch ev.Type {
			case "message_start":
				var ms llm.MessageStart
				if err := json.Unmarshal(ev.Payload, &ms); err != nil {
					logger.Warn("unmarshal message_start failed", "error", err)
				} else {
					result.usage.InputTokens = ms.Message.Usage.InputTokens
				}

			case "content_block_start":
				var cbs llm.ContentBlockStart
				if err := json.Unmarshal(ev.Payload, &cbs); err != nil {
					logger.Warn("unmarshal content_block_start failed", "error", err)
				} else {
					blockIndex = cbs.Index
					currentBlock = &blockBuilder{block: cbs.ContentBlock}
				}

			case "content_block_delta":
				var cbd llm.ContentBlockDelta
				if err := json.Unmarshal(ev.Payload, &cbd); err != nil {
					logger.Warn("unmarshal content_block_delta failed", "error", err)
				} else if currentBlock == nil { //nolint:gocritic // ifElseChain — first branch uses :=, cannot be switch
					logger.Warn("content_block_delta without active block", "index", cbd.Index)
				} else if cbd.Index != blockIndex {
					logger.Warn("content_block_delta index mismatch",
						"expected", blockIndex, "got", cbd.Index)
				} else {
					switch cbd.Delta.Type {
					case "text_delta":
						currentBlock.block.Text += cbd.Delta.Text
						if hooks.OnTextDelta != nil && cbd.Delta.Text != "" {
							hooks.OnTextDelta(cbd.Delta.Text)
						}
					case "thinking_delta":
						// Extended thinking content — accumulate but don't emit to user.
						currentBlock.block.Text += cbd.Delta.Text
						if hooks.OnThinking != nil {
							hooks.OnThinking()
						}
					case "input_json_delta":
						currentBlock.jsonBuf = append(currentBlock.jsonBuf, cbd.Delta.PartialJSON...)
					}
				}

			case "content_block_stop":
				if currentBlock != nil {
					// Finalize the block.
					if currentBlock.block.Type == "tool_use" && len(currentBlock.jsonBuf) > 0 {
						currentBlock.block.Input = json.RawMessage(currentBlock.jsonBuf)
					}
					result.contentBlocks = append(result.contentBlocks, currentBlock.block)
					switch currentBlock.block.Type {
					case "tool_use":
						result.toolCalls = append(result.toolCalls, currentBlock.block)
					case "text":
						result.text += currentBlock.block.Text
					case "thinking":
						// Thinking blocks are part of extended thinking; preserve
						// in contentBlocks but don't include in user-visible text.
						currentBlock.block.Thinking = currentBlock.block.Text
						currentBlock.block.Text = ""
					}
					currentBlock = nil
				}

			case "message_delta":
				var md llm.MessageDelta
				if err := json.Unmarshal(ev.Payload, &md); err != nil {
					logger.Warn("unmarshal message_delta failed", "error", err)
				} else {
					result.stopReason = md.Delta.StopReason
					result.usage.OutputTokens = md.Usage.OutputTokens
				}

			case "message_stop":
				// Stream complete for this turn.
				return nil

			case "error":
				return fmt.Errorf("stream error: %s", string(ev.Payload))
			}
		}
	}
}

// executeOneTool runs a single tool call and returns the tool_result content block.
// Used by both the legacy (post-stream) and streaming (during-stream) dispatch paths.
func executeOneTool(
	ctx context.Context,
	tc llm.ContentBlock,
	tools ToolExecutor,
	hooks StreamHooks,
	turnReason string,
	turn int,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
	loopDetector *ToolLoopDetector,
) llm.ContentBlock {
	if hooks.OnToolStart != nil {
		hooks.OnToolStart(tc.Name, turnReason, tc.Input)
	}
	if hooks.OnToolEmit != nil {
		hooks.OnToolEmit(tc.Name, tc.ID)
	}
	logger.Info("exec", "name", tc.Name, "turn", turn)

	// Tool loop detection: check for stuck patterns before executing.
	if loopDetector != nil {
		loopResult := loopDetector.RecordAndCheck(tc.Name, tc.Input)
		if loopResult.Stuck {
			if loopResult.Level == ToolLoopCritical {
				logger.Warn("tool loop blocked",
					"name", tc.Name, "detector", loopResult.Detector, "count", loopResult.Count)
				result := llm.ContentBlock{
					Type:      "tool_result",
					ToolUseID: tc.ID,
					Content:   loopResult.Message,
					IsError:   true,
				}
				if hooks.OnToolResult != nil {
					hooks.OnToolResult(tc.Name, tc.ID, loopResult.Message, true)
				}
				return result
			}
			// Warning level: inject the warning as a prefix but allow execution.
			logger.Warn("tool loop warning",
				"name", tc.Name, "detector", loopResult.Detector, "count", loopResult.Count)
		}
	}

	// Plugin hook: allow blocking tool execution before it starts.
	if hooks.OnBeforeToolCall != nil {
		if block, reason := hooks.OnBeforeToolCall(tc.Name, tc.ID, tc.Input); block {
			logger.Info("tool blocked by hook", "name", tc.Name, "reason", reason)
			result := llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   fmt.Sprintf("Tool blocked: %s", reason),
				IsError:   true,
			}
			if hooks.OnToolResult != nil {
				hooks.OnToolResult(tc.Name, tc.ID, reason, true)
			}
			return result
		}
	}

	start := time.Now()
	var toolOutput string
	var toolErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				toolErr = fmt.Errorf("tool executor panic: %v", r)
				logger.Error("tool executor panic", "name", tc.Name, "panic", r)
			}
		}()
		if tools != nil {
			toolOutput, toolErr = tools.Execute(ctx, tc.Name, tc.Input)
		} else {
			toolErr = fmt.Errorf("no tool executor configured")
		}
	}()
	elapsed := time.Since(start)

	block := llm.ContentBlock{
		Type:      "tool_result",
		ToolUseID: tc.ID,
	}
	if toolErr != nil {
		block.Content = fmt.Sprintf("Error: %s", toolErr.Error())
		block.IsError = true
	} else {
		block.Content = toolOutput
	}

	// Record result hash for no-progress detection.
	if loopDetector != nil {
		loopDetector.RecordResult(tc.Name, block.Content, block.IsError)
	}

	// Broadcast tool result to streaming clients.
	if hooks.OnToolResult != nil {
		hooks.OnToolResult(tc.Name, tc.ID, block.Content, block.IsError)
	}

	// Log tool execution to agent detail log.
	if runLog != nil {
		td := agentlog.TurnToolData{
			Turn:       turn + 1,
			Name:       tc.Name,
			DurationMs: elapsed.Milliseconds(),
			OutputLen:  len(block.Content),
			IsError:    block.IsError,
		}
		if block.IsError {
			td.Error = block.Content
		}
		runLog.LogTurnTool(td)
	}
	return block
}

// extractThinkingText returns the raw reasoning text from a turn's content
// blocks. Prefers thinking blocks (Anthropic extended thinking), but falls
// back to the last text block (OpenAI-compatible models that explain their
// reasoning in plain text before tool calls). The caller (e.g. channel adapters
// channel adapters) is responsible for summarizing it.
func extractThinkingText(blocks []llm.ContentBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Thinking != "" {
			return blocks[i].Thinking
		}
	}
	// Fallback: use the last text block as reasoning context.
	// OpenAI-compatible models express intent in text before tool calls.
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type == "text" && blocks[i].Text != "" {
			return blocks[i].Text
		}
	}
	return ""
}

// stopReasonFromCtx determines the stop reason from a cancelled context.
func stopReasonFromCtx(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	return "aborted"
}

// stripBase64ImagesFromHistory replaces base64-encoded image blocks in the
// message history with a lightweight text placeholder. Called after turn 0
// when StripImagesAfterFirstTurn is set so that subsequent turns don't
// retransmit large image payloads to the LLM.
//
// Only "base64" source images are stripped; URL-referenced images are left
// intact because they don't carry inline bytes.
func stripBase64ImagesFromHistory(messages []llm.Message) []llm.Message {
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	for i, msg := range result {
		// Only process user messages; assistant/tool messages never contain images.
		if msg.Role != "user" {
			continue
		}

		// Parse as content block array. If it's a plain string there are no images.
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}

		changed := false
		for j, b := range blocks {
			if b.Type == "image" && b.Source != nil && b.Source.Type == "base64" {
				// Replace the heavy data payload with a text note.
				blocks[j] = llm.ContentBlock{
					Type: "text",
					Text: fmt.Sprintf("[image/%s already analyzed — not retransmitted]", b.Source.MediaType),
				}
				changed = true
			}
		}

		if changed {
			newContent, err := json.Marshal(blocks)
			if err == nil {
				result[i] = llm.Message{
					Role:    msg.Role,
					Content: newContent,
				}
			}
		}
	}

	return result
}
