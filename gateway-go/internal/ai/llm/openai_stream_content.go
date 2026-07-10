// openai_stream_content.go — Anthropic-style content block lifecycle for the
// OpenAI stream translator. The protocol loop stays in openai_stream.go; this
// type owns block indices, visible text/thinking transitions, and buffered tool
// calls that must be emitted contiguously for the single-active-block consumer.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

type openAIToolFlushPolicy uint8

const (
	openAIFlushAllTools openAIToolFlushPolicy = iota
	openAIFlushValidTools
	openAIDiscardTools
)

type openAIToolBuilder struct {
	id       string
	name     string
	args     []byte
	blockIdx int
}

// openAIContentEmitter translates OpenAI choice deltas into a contiguous
// Anthropic-style content-block stream. OpenAI may interleave tool argument
// fragments across call indices, while the downstream consumer tracks only one
// active block, so tool calls remain buffered until a terminal flush.
type openAIContentEmitter struct {
	ctx    context.Context
	out    chan<- StreamEvent
	logger *slog.Logger

	nextBlockIndex    int
	textBlockOpen     bool
	textBlockIndex    int
	thinkingBlockOpen bool
	thinkingBlockIdx  int

	toolBuilders map[int]*openAIToolBuilder
	toolOrder    []int
}

func newOpenAIContentEmitter(ctx context.Context, out chan<- StreamEvent, logger *slog.Logger) *openAIContentEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &openAIContentEmitter{
		ctx:              ctx,
		out:              out,
		logger:           logger,
		textBlockIndex:   -1,
		thinkingBlockIdx: -1,
		toolBuilders:     make(map[int]*openAIToolBuilder),
	}
}

// emitText routes s into the lazily opened text block, closing an active
// thinking block first so every emitted block remains contiguous.
func (e *openAIContentEmitter) emitText(s string) {
	if e.thinkingBlockOpen {
		e.thinkingBlockOpen = false
		e.closeBlock(e.thinkingBlockIdx)
	}
	if !e.textBlockOpen {
		e.textBlockOpen = true
		e.textBlockIndex = e.allocateBlockIndex()
		payload, _ := json.Marshal(ContentBlockStart{
			Index:        e.textBlockIndex,
			ContentBlock: ContentBlock{Type: "text"},
		})
		emit(e.ctx, e.out, StreamEvent{Type: "content_block_start", Payload: payload})
	}
	e.emitDelta(e.textBlockIndex, "text_delta", s, "")
}

// emitThinking routes s into the lazily opened thinking block. Providers
// occasionally send text before reasoning, so an active text block is closed
// before allocating a distinct thinking block index.
func (e *openAIContentEmitter) emitThinking(s string) {
	if !e.thinkingBlockOpen {
		if e.textBlockOpen {
			e.textBlockOpen = false
			e.closeBlock(e.textBlockIndex)
		}
		e.thinkingBlockOpen = true
		e.thinkingBlockIdx = e.allocateBlockIndex()
		payload, _ := json.Marshal(ContentBlockStart{
			Index:        e.thinkingBlockIdx,
			ContentBlock: ContentBlock{Type: "thinking"},
		})
		emit(e.ctx, e.out, StreamEvent{Type: "content_block_start", Payload: payload})
	}
	e.emitDelta(e.thinkingBlockIdx, "thinking_delta", s, "")
}

// appendToolCalls accumulates streamed tool fragments by provider call index.
// The first fragment reserves the output block index and first-seen order;
// later fragments may fill in an omitted id/name and extend the arguments.
func (e *openAIContentEmitter) appendToolCalls(calls []openAIDeltaToolCall) {
	for _, call := range calls {
		builder, exists := e.toolBuilders[call.Index]
		if !exists {
			// A visible text/thinking block must finish before the first buffered
			// tool block reserves its place in the block sequence.
			e.closeVisible()
			builder = &openAIToolBuilder{
				id:       call.ID,
				name:     call.Function.Name,
				blockIdx: e.allocateBlockIndex(),
			}
			e.toolBuilders[call.Index] = builder
			e.toolOrder = append(e.toolOrder, call.Index)
		} else {
			if call.ID != "" {
				builder.id = call.ID
			}
			if call.Function.Name != "" {
				builder.name = call.Function.Name
			}
		}
		builder.args = append(builder.args, call.Function.Arguments...)
	}
}

// closeVisible stops any active thinking/text block. Both guards are retained
// defensively even though normal transitions keep at most one visible block
// open at a time.
func (e *openAIContentEmitter) closeVisible() {
	if e.thinkingBlockOpen {
		e.thinkingBlockOpen = false
		e.closeBlock(e.thinkingBlockIdx)
	}
	if e.textBlockOpen {
		e.textBlockOpen = false
		e.closeBlock(e.textBlockIndex)
	}
}

// flushTools applies the terminal tool policy, emits accepted calls in
// first-seen order as contiguous blocks, and always clears the buffer.
func (e *openAIContentEmitter) flushTools(policy openAIToolFlushPolicy) {
	defer func() {
		e.toolBuilders = make(map[int]*openAIToolBuilder)
		e.toolOrder = nil
	}()

	if policy == openAIDiscardTools {
		return
	}
	for _, index := range e.toolOrder {
		builder := e.toolBuilders[index]
		if policy == openAIFlushValidTools && len(builder.args) > 0 && !json.Valid(builder.args) {
			e.logger.Warn("dropping tool call with truncated arguments at premature stream end",
				"tool", builder.name, "argsLen", len(builder.args))
			continue
		}
		if builder.id == "" {
			// Some OpenAI-compatible servers omit tool ids. Pairing tool_use
			// with tool_result still requires a stable, non-empty id.
			builder.id = fmt.Sprintf("call_%d", builder.blockIdx)
		}
		start, _ := json.Marshal(ContentBlockStart{
			Index: builder.blockIdx,
			ContentBlock: ContentBlock{
				Type: "tool_use",
				ID:   builder.id,
				Name: builder.name,
			},
		})
		emit(e.ctx, e.out, StreamEvent{Type: "content_block_start", Payload: start})
		if len(builder.args) > 0 {
			e.emitDelta(builder.blockIdx, "input_json_delta", "", string(builder.args))
		}
		e.closeBlock(builder.blockIdx)
	}
}

func (e *openAIContentEmitter) bufferedToolCount() int {
	return len(e.toolOrder)
}

func (e *openAIContentEmitter) allocateBlockIndex() int {
	index := e.nextBlockIndex
	e.nextBlockIndex++
	return index
}

func (e *openAIContentEmitter) closeBlock(index int) {
	payload, _ := json.Marshal(ContentBlockStop{Index: index})
	emit(e.ctx, e.out, StreamEvent{Type: "content_block_stop", Payload: payload})
}

func (e *openAIContentEmitter) emitDelta(index int, deltaType, text, partialJSON string) {
	var delta ContentBlockDelta
	delta.Index = index
	delta.Delta.Type = deltaType
	delta.Delta.Text = text
	delta.Delta.PartialJSON = partialJSON
	payload, _ := json.Marshal(delta)
	emit(e.ctx, e.out, StreamEvent{Type: "content_block_delta", Payload: payload})
}
