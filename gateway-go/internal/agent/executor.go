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
	"sync"
	"time"

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

	// Zen arch — Superscalar Unit Separation: wrap hooks with async dispatch
	// so stream parsing (frontend) and hook execution (backend) run on
	// independent goroutines. The parser never stalls on slow hooks.
	//
	// IMPORTANT: Close() must be called AFTER all tool goroutines finish,
	// because tool goroutines enqueue hook events (OnToolResult). Closing
	// the queue while goroutines are still writing would panic.
	var hookDispatcher *AsyncHookDispatcher
	hookDispatcher, hooks = NewAsyncHookDispatcher(hooks)
	// NOT deferred here — closed explicitly after the loop to ensure
	// all tool goroutines have finished writing to the hook queue.

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

		// Zen arch — Superscalar Early Issue: start tool execution as soon as
		// each tool_use block completes in the stream, rather than waiting for
		// the entire stream to finish. This overlaps remaining stream parsing
		// (and other tool_use block accumulation) with tool execution.
		//
		// CPU analogy: superscalar processors issue instructions to execution
		// units as soon as operands are ready, not after the entire fetch
		// window completes.
		var earlyTools []earlyToolEntry
		var earlyMu sync.Mutex
		var wg sync.WaitGroup

		onToolReady := func(tc llm.ContentBlock) {
			earlyMu.Lock()
			idx := len(earlyTools)
			earlyTools = append(earlyTools, earlyToolEntry{})
			earlyMu.Unlock()

			wg.Add(1)
			go func(idx int, tc llm.ContentBlock) {
				defer wg.Done()
				if hooks.OnToolStart != nil {
					hooks.OnToolStart(tc.Name, "") // reason filled post-stream
				}
				if hooks.OnToolEmit != nil {
					hooks.OnToolEmit(tc.Name, tc.ID)
				}
				logger.Info("exec-early", "name", tc.Name, "turn", turn)

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

				if hooks.OnToolResult != nil {
					hooks.OnToolResult(tc.Name, tc.ID, block.Content, block.IsError)
				}
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

				earlyMu.Lock()
				earlyTools[idx] = earlyToolEntry{id: tc.ID, result: block}
				earlyMu.Unlock()
			}(idx, tc)
		}

		// Consume the stream, launching tool goroutines as tool_use blocks complete.
		turnRes, err := consumeStream(ctx, events, hooks, onToolReady)

		// ALWAYS wait for tool goroutines before proceeding, even on error.
		// This prevents: (a) goroutine leaks, (b) writes to closed hook queue,
		// (c) race on earlyTools slice.
		wg.Wait()

		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				hookDispatcher.Close()
				return result, nil
			}
			hookDispatcher.Close()
			return nil, fmt.Errorf("consume stream (turn %d): %w", turn, err)
		}

		// Accumulate usage.
		result.Usage.InputTokens += turnRes.usage.InputTokens
		result.Usage.OutputTokens += turnRes.usage.OutputTokens

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

		if cfg.OnTurn != nil {
			cfg.OnTurn(turn+1, result.Usage.InputTokens+result.Usage.OutputTokens)
		}

		if turnRes.text != "" {
			result.Text = turnRes.text
		}

		// Check stop reason.
		if turnRes.stopReason == "end_turn" || len(turnRes.toolCalls) == 0 {
			result.StopReason = turnRes.stopReason
			if result.StopReason == "" {
				result.StopReason = "end_turn"
			}
			hookDispatcher.Close()
			return result, nil
		}

		// Build assistant message with all content blocks from this turn.
		messages = append(messages, llm.NewBlockMessage("assistant", turnRes.contentBlocks))

		// Assemble tool results in the order LLM expects (matching toolCalls order).
		// Safe to read earlyTools without lock — wg.Wait() above guarantees all
		// tool goroutines have finished writing.
		toolResults := buildToolResults(turnRes.toolCalls, earlyTools)
		messages = append(messages, llm.NewBlockMessage("user", toolResults))
	}

	result.StopReason = "max_turns"
	hookDispatcher.Close()
	return result, nil
}

// earlyToolEntry tracks a tool that was dispatched during stream parsing.
type earlyToolEntry struct {
	id     string
	result llm.ContentBlock
}

// buildToolResults assembles tool results in the order the LLM expects,
// matching the original toolCalls order. Early-dispatched tools are looked
// up by tool_use_id.
func buildToolResults(toolCalls []llm.ContentBlock, early []earlyToolEntry) []llm.ContentBlock {
	byID := make(map[string]llm.ContentBlock, len(early))
	for _, e := range early {
		if e.id != "" {
			byID[e.id] = e.result
		}
	}
	results := make([]llm.ContentBlock, len(toolCalls))
	for i, tc := range toolCalls {
		if r, ok := byID[tc.ID]; ok {
			results[i] = r
		} else {
			// Should not happen — all tools dispatched via onToolReady.
			results[i] = llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   "Error: tool result not found",
				IsError:   true,
			}
		}
	}
	return results
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
//
// onToolReady is called (if non-nil) when a tool_use content block is fully
// parsed — enabling early tool dispatch while the stream continues.
func consumeStream(ctx context.Context, events <-chan llm.StreamEvent, hooks StreamHooks, onToolReady func(llm.ContentBlock)) (*turnResult, error) {
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
						// Zen arch — Early Issue: dispatch tool execution immediately
						// while the stream continues parsing remaining blocks.
						if onToolReady != nil {
							onToolReady(currentBlock.block)
						}
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

// extractThinkingText returns the raw reasoning text from a turn's content
// blocks. Prefers thinking blocks (Anthropic extended thinking), but falls
// back to the last text block (OpenAI-compatible models that explain their
// reasoning in plain text before tool calls). The caller (e.g. Discord
// ProgressTracker) is responsible for summarizing it.
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
