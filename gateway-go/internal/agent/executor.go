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
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
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
		cfg.Timeout = 10 * time.Minute
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

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		result.Turns = turn + 1

		// Per-turn context initialization (e.g., injecting a TurnContext for
		// cross-tool result sharing). The caller sets cfg.OnTurnInit to provide
		// this behaviour; the executor itself stays context-agnostic.
		if cfg.OnTurnInit != nil {
			ctx = cfg.OnTurnInit(ctx)
		}

		req := llm.ChatRequest{
			Model:     cfg.Model,
			Messages:  messages,
			System:    cfg.System,
			MaxTokens: cfg.MaxTokens,
			Tools:     cfg.Tools,
			Stream:    true,
		}

		var events <-chan llm.StreamEvent
		var err error
		if cfg.APIType == "anthropic" {
			events, err = client.StreamChat(ctx, req)
		} else {
			// Default: OpenAI-compatible API (covers openai, zai, sglang, etc.)
			events, err = client.StreamChatOpenAI(ctx, req)
		}
		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				return result, nil
			}
			return nil, fmt.Errorf("stream chat (turn %d): %w", turn, err)
		}

		// Consume the stream for this turn.
		turnRes, err := consumeStream(ctx, events, hooks)
		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				return result, nil
			}
			return nil, fmt.Errorf("consume stream (turn %d): %w", turn, err)
		}

		// Accumulate usage.
		result.Usage.InputTokens += turnRes.usage.InputTokens
		result.Usage.OutputTokens += turnRes.usage.OutputTokens

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

		// Keep latest text as the final text.
		if turnRes.text != "" {
			result.Text = turnRes.text
		}

		// Check stop reason.
		if turnRes.stopReason == "end_turn" || len(turnRes.toolCalls) == 0 {
			result.StopReason = turnRes.stopReason
			if result.StopReason == "" {
				result.StopReason = "end_turn"
			}
			return result, nil
		}

		// Build assistant message with all content blocks from this turn.
		messages = append(messages, llm.NewBlockMessage("assistant", turnRes.contentBlocks))

		// Extract a brief reasoning summary from the turn's thinking blocks
		// so Discord can display what the agent is thinking alongside each tool step.
		turnReason := extractThinkingSummary(turnRes.contentBlocks)

		// Execute tools in parallel and build tool_result blocks.
		// Each goroutine writes to its own index — no mutex needed for the slice.
		toolResults := make([]llm.ContentBlock, len(turnRes.toolCalls))
		var wg sync.WaitGroup
		for i, tc := range turnRes.toolCalls {
			wg.Add(1)
			go func(idx int, tc llm.ContentBlock) {
				defer wg.Done()
				if hooks.OnToolStart != nil {
					hooks.OnToolStart(tc.Name, turnReason)
				}
				if hooks.OnToolEmit != nil {
					hooks.OnToolEmit(tc.Name, tc.ID)
				}
				logger.Info("exec", "name", tc.Name, "turn", turn)

				start := time.Now()
				var toolOutput string
				var toolErr error
				if tools != nil {
					toolOutput, toolErr = tools.Execute(ctx, tc.Name, tc.Input)
				} else {
					toolErr = fmt.Errorf("no tool executor configured")
				}
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
				toolResults[idx] = block

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
			}(i, tc)
		}

		// Wait for all tool goroutines, but bail immediately if ctx is cancelled
		// (agent abort/kill). The goroutines still run to completion in the
		// background — they hold their own index and don't share state.
		wgDone := make(chan struct{})
		go func() { wg.Wait(); close(wgDone) }()
		select {
		case <-wgDone:
		case <-ctx.Done():
			result.StopReason = stopReasonFromCtx(ctx)
			return result, nil
		}

		if ctx.Err() != nil {
			result.StopReason = stopReasonFromCtx(ctx)
			return result, nil
		}

		messages = append(messages, llm.NewBlockMessage("user", toolResults))
	}

	result.StopReason = "max_turns"
	return result, nil
}

// turnResult holds the parsed output of a single LLM turn.
type turnResult struct {
	text          string
	stopReason    string
	toolCalls     []llm.ContentBlock
	contentBlocks []llm.ContentBlock
	usage         llm.TokenUsage
}

// consumeStream reads all events from a streaming LLM response and assembles
// the turn result. Handles both Anthropic and OpenAI SSE formats.
func consumeStream(ctx context.Context, events <-chan llm.StreamEvent, hooks StreamHooks) (*turnResult, error) {
	result := &turnResult{}

	// Track current content block being built.
	type blockBuilder struct {
		block   llm.ContentBlock
		jsonBuf []byte // accumulator for input_json_delta
	}
	var currentBlock *blockBuilder
	var blockIndex int = -1

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return result, nil
			}

			switch ev.Type {
			case "message_start":
				var ms llm.MessageStart
				if json.Unmarshal(ev.Payload, &ms) == nil {
					result.usage.InputTokens = ms.Message.Usage.InputTokens
				}

			case "content_block_start":
				var cbs llm.ContentBlockStart
				if json.Unmarshal(ev.Payload, &cbs) == nil {
					blockIndex = cbs.Index
					currentBlock = &blockBuilder{block: cbs.ContentBlock}
				}

			case "content_block_delta":
				var cbd llm.ContentBlockDelta
				if json.Unmarshal(ev.Payload, &cbd) == nil && currentBlock != nil && cbd.Index == blockIndex {
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
				if json.Unmarshal(ev.Payload, &md) == nil {
					result.stopReason = md.Delta.StopReason
					result.usage.OutputTokens = md.Usage.OutputTokens
				}

			case "message_stop":
				// Stream complete for this turn.
				return result, nil

			case "error":
				return result, fmt.Errorf("stream error: %s", string(ev.Payload))
			}
		}
	}
}

// extractThinkingSummary returns a brief summary from the last thinking block
// in a turn's content blocks. Used to show LLM reasoning alongside tool steps.
// Returns empty string if no thinking block is found.
func extractThinkingSummary(blocks []llm.ContentBlock) string {
	// Find the last thinking block in this turn.
	var thinkingText string
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Thinking != "" {
			thinkingText = blocks[i].Thinking
			break
		}
	}
	if thinkingText == "" {
		return ""
	}

	// Extract a brief summary: take the last non-empty line (closest to the
	// tool decision) and truncate to keep the progress embed compact.
	lines := strings.Split(strings.TrimSpace(thinkingText), "\n")
	var summary string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && !strings.HasPrefix(line, "```") {
			summary = line
			break
		}
	}
	if summary == "" {
		return ""
	}

	// Strip leading markers like "- ", "* ", "1. " etc.
	summary = strings.TrimLeft(summary, "-*•→> ")
	summary = strings.TrimSpace(summary)

	const maxRunes = 60
	if utf8.RuneCountInString(summary) > maxRunes {
		runes := []rune(summary)
		summary = string(runes[:maxRunes]) + "…"
	}
	return summary
}

// stopReasonFromCtx determines the stop reason from a cancelled context.
func stopReasonFromCtx(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	return "aborted"
}
