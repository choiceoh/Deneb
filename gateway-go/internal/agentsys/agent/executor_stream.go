// executor_stream.go — per-turn LLM stream consumption: turnResult
// accumulation, the idle-timeout watchdog (ErrStreamIdle), and streaming
// hook dispatch. Split from executor.go (RunAgent core loop).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type turnResult struct {
	text          string
	stopReason    string
	toolCalls     []llm.ContentBlock
	contentBlocks []llm.ContentBlock
	usage         llm.TokenUsage
}

// defaultStreamIdleTimeout is the default maximum wait for the next SSE event
// during LLM streaming. Set above Claude Code's 90s default because local vLLM
// models (step3p7) have a slow cold start (~80s) and slow prefill on large
// contexts; at 90s their first token can miss the window and trip a false idle
// stall, which retried and failed cron runs (email-realtime auto-disabled after
// 10 such errors). Fast hosted APIs (GLM via Z.ai) stream well within this, so
// the higher ceiling only delays detection of a genuine hang.
const defaultStreamIdleTimeout = 180 * time.Second

// ErrStreamIdle is returned when the LLM stream stalls (no event within the
// idle timeout). The error is considered retryable by callers.
var ErrStreamIdle = fmt.Errorf("stream stalled: no event within idle timeout")

// ErrStreamEvent is returned when the provider emits an explicit error event
// mid-stream (upstream disconnect, transient backend fault, overload). Like
// ErrStreamIdle it is considered retryable: mid-stream errors are almost
// always transient (permanent faults reject the request at the HTTP layer,
// before streaming starts), so callers retry the turn once on the same model
// before escalating to the model-fallback chain.
var ErrStreamEvent = errors.New("stream reported error event")

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
	blockIndex := -1

	// finalizePending appends the in-flight block to the result, applying the
	// same field finalization content_block_stop performs (tool args, the
	// thinking Text→Thinking move). No-op when no block is open.
	finalizePending := func() {
		if currentBlock == nil {
			return
		}
		// Finalize the block's fields BEFORE appending — result.contentBlocks
		// takes a value copy, so any mutation after the append lands on the
		// soon-discarded currentBlock.block and is lost.
		switch currentBlock.block.Type {
		case "tool_use":
			if len(currentBlock.jsonBuf) > 0 {
				currentBlock.block.Input = json.RawMessage(currentBlock.jsonBuf)
			}
		case "thinking":
			// Extended-thinking content streams in as thinking_delta and was
			// accumulated into Text; move it to Thinking (where the round-trip
			// and joinAllThinkingTexts read it) and clear Text so it stays out
			// of user-visible output. Must happen before the append.
			currentBlock.block.Thinking = currentBlock.block.Text
			currentBlock.block.Text = ""
		}
		result.contentBlocks = append(result.contentBlocks, currentBlock.block)
		switch currentBlock.block.Type {
		case "tool_use":
			result.toolCalls = append(result.toolCalls, currentBlock.block)
		case "text":
			result.text += currentBlock.block.Text
		}
		currentBlock = nil
	}

	// flushTruncated rescues an un-stopped in-flight block when the stream
	// ends without its content_block_stop — a mid-stream EOF, a lost finish
	// chunk, or [DONE] arriving with a block still open. Text/thinking the
	// user already watched stream in (OnTextDelta fired live) must survive
	// into the persisted result instead of silently vanishing into an empty
	// reply. An incomplete tool_use is kept only when its accumulated
	// arguments are complete valid JSON; executing truncated arguments would
	// perform a half-specified action.
	flushTruncated := func() {
		if currentBlock == nil {
			return
		}
		if currentBlock.block.Type == "tool_use" &&
			len(currentBlock.jsonBuf) > 0 && !json.Valid(currentBlock.jsonBuf) {
			logger.Warn("dropping incomplete tool_use at truncated stream end",
				"tool", currentBlock.block.Name, "argsLen", len(currentBlock.jsonBuf))
			currentBlock = nil
			return
		}
		logger.Warn("finalizing un-stopped block at truncated stream end",
			"type", currentBlock.block.Type)
		finalizePending()
	}

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
				// Channel closed without message_stop — truncated stream.
				flushTruncated()
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
					result.usage.CacheReadInputTokens = ms.Message.Usage.CacheReadInputTokens
					result.usage.CacheCreationInputTokens = ms.Message.Usage.CacheCreationInputTokens
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
						// Anthropic-native sends the chunk in `thinking`; OpenAI-translated
						// SSE puts it in `text`. Accept whichever is non-empty.
						chunk := cbd.Delta.Thinking
						if chunk == "" {
							chunk = cbd.Delta.Text
						}
						currentBlock.block.Text += chunk
						if hooks.OnThinking != nil {
							hooks.OnThinking(chunk)
						}
					case "signature_delta":
						// Anthropic attaches a cryptographic signature at the end of a
						// thinking block. Accumulate so it can round-trip on later turns.
						currentBlock.block.Signature += cbd.Delta.Signature
					case "input_json_delta":
						currentBlock.jsonBuf = append(currentBlock.jsonBuf, cbd.Delta.PartialJSON...)
					}
				}

			case "content_block_stop":
				finalizePending()

			case "message_delta":
				var md llm.MessageDelta
				if err := json.Unmarshal(ev.Payload, &md); err != nil {
					logger.Warn("unmarshal message_delta failed", "error", err)
				} else {
					// Don't let a trailing usage-only message_delta (stop_reason
					// decodes to "") clobber a real stop_reason set by an earlier
					// delta — that would, e.g., defeat the max_tokens resume path.
					// Mirrors the cache-token guards just below.
					if md.Delta.StopReason != "" {
						result.stopReason = md.Delta.StopReason
					}
					result.usage.OutputTokens = md.Usage.OutputTokens
					// Some Anthropic endpoints report final cache totals only
					// on message_delta. Accept them, but do not clobber a
					// non-zero message_start value with a missing one.
					if md.Usage.CacheReadInputTokens > 0 {
						result.usage.CacheReadInputTokens = md.Usage.CacheReadInputTokens
					}
					if md.Usage.CacheCreationInputTokens > 0 {
						result.usage.CacheCreationInputTokens = md.Usage.CacheCreationInputTokens
					}
				}

			case "message_stop":
				// Stream complete for this turn. A block still open here means the
				// stream was truncated (translators only reach message_stop with an
				// open block on [DONE]/EOF without a finish chunk) — rescue it.
				flushTruncated()
				return nil

			case "error":
				return fmt.Errorf("%w: %s", ErrStreamEvent, string(ev.Payload))
			}
		}
	}
}

// executeOneTool runs a single tool call and returns the tool_result content block.
// Used by both the legacy (post-stream) and streaming (during-stream) dispatch paths.
