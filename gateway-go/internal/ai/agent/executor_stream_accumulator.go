// executor_stream_accumulator.go — event-to-turn state accumulation for one
// LLM stream. Transport waiting and idle watchdog ownership stay in
// consumeStreamInto; this type owns only stream protocol state.
package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type streamBlockBuilder struct {
	block   llm.ContentBlock
	jsonBuf []byte
	textBuf strings.Builder
}

type streamAccumulator struct {
	result       *turnResult
	hooks        StreamHooks
	logger       *slog.Logger
	currentBlock *streamBlockBuilder
	blockIndex   int
}

func newStreamAccumulator(result *turnResult, hooks StreamHooks, logger *slog.Logger) *streamAccumulator {
	if logger == nil {
		logger = slog.Default()
	}
	return &streamAccumulator{
		result:     result,
		hooks:      hooks,
		logger:     logger,
		blockIndex: -1,
	}
}

// apply folds one provider event into the turn result. complete is true only
// for message_stop; an exhausted raw event channel is handled by the caller.
func (a *streamAccumulator) apply(event llm.StreamEvent) (complete bool, err error) {
	if a.result.maxStreamBytes > 0 {
		incoming := len(event.Type) + event.Payload.Len()
		if incoming > a.result.maxStreamBytes-a.result.streamBytes {
			return false, ErrStreamLimit
		}
		a.result.streamBytes += incoming
	}

	switch event.Type {
	case "message_start":
		if err := a.applyMessageStart(json.RawMessage(event.Payload.Bytes())); err != nil {
			return false, err
		}
	case "content_block_start":
		a.startContentBlock(json.RawMessage(event.Payload.Bytes()))
	case "content_block_delta":
		a.applyContentBlockDelta(json.RawMessage(event.Payload.Bytes()))
	case "content_block_stop":
		a.finalizePending()
	case "message_delta":
		a.applyMessageDelta(json.RawMessage(event.Payload.Bytes()))
	case "message_stop":
		// A block still open here means the stream ended without its finish
		// chunk. Preserve already-streamed text/thinking where it is safe.
		a.flushTruncated()
		return true, nil
	case "error":
		return false, fmt.Errorf("%w: %s", ErrStreamEvent, event.Payload.String())
	}
	return false, nil
}

func (a *streamAccumulator) applyMessageStart(payload json.RawMessage) error {
	var messageStart llm.MessageStart
	if err := json.Unmarshal(payload, &messageStart); err != nil {
		a.logger.Warn("unmarshal message_start failed", "error", err)
		return nil
	}

	providerModel := strings.TrimSpace(messageStart.Message.Model)
	if providerModel != "" {
		if a.result.providerModel != "" && a.result.providerModel != providerModel {
			return fmt.Errorf("provider model changed within turn from %q to %q", a.result.providerModel, providerModel)
		}
		a.result.providerModel = providerModel
	}
	usage := messageStart.Message.Usage
	a.result.usage.InputTokens = usage.InputTokens
	a.result.usage.CacheReadInputTokens = usage.CacheReadInputTokens
	a.result.usage.CacheCreationInputTokens = usage.CacheCreationInputTokens
	return nil
}

func (a *streamAccumulator) startContentBlock(payload json.RawMessage) {
	var blockStart llm.ContentBlockStart
	if err := json.Unmarshal(payload, &blockStart); err != nil {
		a.logger.Warn("unmarshal content_block_start failed", "error", err)
		return
	}

	a.result.contentStarted = true
	a.blockIndex = blockStart.Index
	a.currentBlock = &streamBlockBuilder{block: blockStart.ContentBlock}
}

func (a *streamAccumulator) applyContentBlockDelta(payload json.RawMessage) {
	var blockDelta llm.ContentBlockDelta
	if err := json.Unmarshal(payload, &blockDelta); err != nil {
		a.logger.Warn("unmarshal content_block_delta failed", "error", err)
		return
	}
	if a.currentBlock == nil {
		a.logger.Warn("content_block_delta without active block", "index", blockDelta.Index)
		return
	}
	if blockDelta.Index != a.blockIndex {
		a.logger.Warn("content_block_delta index mismatch",
			"expected", a.blockIndex, "got", blockDelta.Index)
		return
	}

	switch blockDelta.Delta.Type {
	case "text_delta":
		a.currentBlock.textBuf.WriteString(blockDelta.Delta.Text)
		if a.hooks.OnTextDelta != nil && blockDelta.Delta.Text != "" {
			a.hooks.OnTextDelta(blockDelta.Delta.Text)
		}
	case "thinking_delta":
		// Anthropic-native uses thinking; OpenAI-translated SSE uses text.
		chunk := blockDelta.Delta.Thinking
		if chunk == "" {
			chunk = blockDelta.Delta.Text
		}
		a.currentBlock.textBuf.WriteString(chunk)
		if a.hooks.OnThinking != nil {
			a.hooks.OnThinking(chunk)
		}
	case "signature_delta":
		a.currentBlock.block.Signature += blockDelta.Delta.Signature
	case "input_json_delta":
		a.currentBlock.jsonBuf = append(a.currentBlock.jsonBuf, blockDelta.Delta.PartialJSON...)
	}
}

func (a *streamAccumulator) applyMessageDelta(payload json.RawMessage) {
	var messageDelta llm.MessageDelta
	if err := json.Unmarshal(payload, &messageDelta); err != nil {
		a.logger.Warn("unmarshal message_delta failed", "error", err)
		return
	}

	// A trailing usage-only delta must not erase an earlier real stop reason.
	if messageDelta.Delta.StopReason != "" {
		a.result.stopReason = messageDelta.Delta.StopReason
	}
	// Output usage is cumulative. A trailing usage-less delta must not erase a
	// larger provider-attested count before hard-budget accounting.
	if messageDelta.Usage.OutputTokens > a.result.usage.OutputTokens {
		a.result.usage.OutputTokens = messageDelta.Usage.OutputTokens
	}
	// Cache totals may be absent from message_delta. Zero means missing here,
	// so retain the message_start value in that case.
	if messageDelta.Usage.CacheReadInputTokens > 0 {
		a.result.usage.CacheReadInputTokens = messageDelta.Usage.CacheReadInputTokens
	}
	if messageDelta.Usage.CacheCreationInputTokens > 0 {
		a.result.usage.CacheCreationInputTokens = messageDelta.Usage.CacheCreationInputTokens
	}
}

// finalizePending applies type-specific fields before appending the block.
// The ordering matters because contentBlocks and toolCalls hold value copies.
func (a *streamAccumulator) finalizePending() {
	if a.currentBlock == nil {
		return
	}

	if a.currentBlock.textBuf.Len() > 0 {
		a.currentBlock.block.Text = a.currentBlock.textBuf.String()
	}
	switch a.currentBlock.block.Type {
	case "tool_use":
		if len(a.currentBlock.jsonBuf) > 0 {
			a.currentBlock.block.Input = llm.FlexibleFromRaw(a.currentBlock.jsonBuf)
		}
	case "thinking":
		a.currentBlock.block.Thinking = a.currentBlock.block.Text
		a.currentBlock.block.Text = ""
	}

	block := a.currentBlock.block
	a.result.contentBlocks = append(a.result.contentBlocks, block)
	switch block.Type {
	case "tool_use":
		a.result.toolCalls = append(a.result.toolCalls, block)
	case "text":
		a.result.text += block.Text
	}
	a.currentBlock = nil
}

// flushTruncated rescues safe in-flight blocks when a stream ends without
// content_block_stop. Incomplete tool JSON is deliberately discarded rather
// than risking a half-specified side effect.
func (a *streamAccumulator) flushTruncated() {
	if a.currentBlock == nil {
		return
	}
	if a.currentBlock.block.Type == "tool_use" &&
		len(a.currentBlock.jsonBuf) > 0 && !json.Valid(a.currentBlock.jsonBuf) {
		a.logger.Warn("dropping incomplete tool_use at truncated stream end",
			"tool", a.currentBlock.block.Name, "argsLen", len(a.currentBlock.jsonBuf))
		a.currentBlock = nil
		return
	}

	a.logger.Warn("finalizing un-stopped block at truncated stream end",
		"type", a.currentBlock.block.Type)
	a.finalizePending()
}
