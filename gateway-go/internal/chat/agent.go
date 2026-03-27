package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// AgentConfig configures the agent execution loop.
type AgentConfig struct {
	MaxTurns  int           // Maximum tool-call turns before stopping. Default: 25.
	Timeout   time.Duration // Maximum wall time for the entire agent run. Default: 10m.
	Model     string
	System    json.RawMessage // System prompt: JSON string or array of ContentBlocks.
	Tools     []llm.Tool
	MaxTokens int    // Max output tokens per LLM call. Default: 8192.
	APIType   string // "openai" (default) or "anthropic"
	OnTurn    TurnCallback // optional; called after each turn for mid-run hooks
}

// TurnCallback is called after each agent turn with accumulated token count.
// Used for mid-conversation memory extraction (Honcho-style).
type TurnCallback func(turn int, accumulatedTokens int)

// DefaultAgentConfig returns sensible defaults.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		MaxTurns:  25,
		Timeout:   10 * time.Minute,
		MaxTokens: 8192,
	}
}

// AgentResult is the outcome of an agent run.
type AgentResult struct {
	Text       string
	StopReason string // "end_turn", "max_tokens", "timeout", "aborted", "max_turns"
	Usage      llm.TokenUsage
	Turns      int
}

// StreamHooks contains optional callbacks for agent streaming events.
// All fields are optional — nil callbacks are silently skipped.
type StreamHooks struct {
	OnTextDelta func(text string) // text delta streamed from LLM
	OnThinking  func()           // reasoning/thinking delta received
	OnToolStart func(name string) // tool invocation about to execute
}

// RunAgent executes the agent tool-call loop: call LLM → detect tool_use →
// execute tool → feed result → repeat until the model stops or limits are hit.
//
// hooks provides optional callbacks for streaming events (text deltas,
// thinking phases, tool starts). Pass nil or zero-value if not needed.
func RunAgent(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client *llm.Client,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
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

		// Create a fresh TurnContext for cross-tool result sharing within this turn.
		turnCtx := NewTurnContext()
		ctx = WithTurnContext(ctx, turnCtx)

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
		turnResult, err := consumeStream(ctx, events, hooks)
		if err != nil {
			if ctx.Err() != nil {
				result.StopReason = stopReasonFromCtx(ctx)
				return result, nil
			}
			return nil, fmt.Errorf("consume stream (turn %d): %w", turn, err)
		}

		// Accumulate usage.
		result.Usage.InputTokens += turnResult.usage.InputTokens
		result.Usage.OutputTokens += turnResult.usage.OutputTokens

		// Mid-run hook: notify caller of token accumulation (for memory extraction).
		if cfg.OnTurn != nil {
			cfg.OnTurn(turn+1, result.Usage.InputTokens+result.Usage.OutputTokens)
		}

		// If there's text output, keep it as the final text.
		if turnResult.text != "" {
			result.Text = turnResult.text
		}

		// Check stop reason.
		if turnResult.stopReason == "end_turn" || len(turnResult.toolCalls) == 0 {
			result.StopReason = turnResult.stopReason
			if result.StopReason == "" {
				result.StopReason = "end_turn"
			}
			return result, nil
		}

		// Build assistant message with all content blocks from this turn.
		assistantBlocks := turnResult.contentBlocks
		messages = append(messages, llm.NewBlockMessage("assistant", assistantBlocks))

		// Execute tools in parallel and build tool_result blocks.
		// Each goroutine writes to its own index — no mutex needed for the slice.
		toolResults := make([]llm.ContentBlock, len(turnResult.toolCalls))
		var wg sync.WaitGroup
		for i, tc := range turnResult.toolCalls {
			if hooks.OnToolStart != nil {
				hooks.OnToolStart(tc.Name)
			}
			wg.Add(1)
			go func(idx int, tc llm.ContentBlock) {
				defer wg.Done()
				logger.Info("executing tool", "name", tc.Name, "turn", turn)

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

				// Store result in TurnContext for cross-tool referencing ($ref).
				turnCtx.Store(tc.ID, &turnResult_{
					ToolName: tc.Name,
					Output:   block.Content,
					IsError:  block.IsError,
					Duration: elapsed,
				})
			}(i, tc)
		}
		wg.Wait()

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
// the turn result.
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
