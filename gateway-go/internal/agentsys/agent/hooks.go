// hooks.go — StreamHooks callbacks for agent streaming events.
package agent

import "github.com/choiceoh/deneb/gateway-go/internal/ai/llm"

// StreamHooks contains optional callbacks for agent streaming events.
// All fields are optional — nil callbacks are silently skipped.
type StreamHooks struct {
	OnTextDelta  func(text string)                                // text delta streamed from LLM
	OnThinking   func()                                           // reasoning/thinking delta received
	OnToolStart  func(name, reason string, input []byte)          // tool invocation about to execute; reason is thinking text, input is raw JSON args
	OnToolEmit   func(name, toolUseID string)                     // tool start broadcast (name + ID for streaming)
	OnToolResult func(name, toolUseID, result string, isErr bool) // tool result broadcast
	// OnBeforeToolCall is called before each tool execution. Returns true to
	// block the tool call (with blockReason as the tool output).
	OnBeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)
	// OnToolBlockReady is called during streaming when a tool_use content block
	// is fully received (on content_block_stop), before the stream ends. When
	// set, enables streaming tool dispatch: the callback should start tool
	// execution immediately. The index is the 0-based position within the turn's
	// tool calls. When nil, tools are dispatched after the stream completes.
	OnToolBlockReady func(toolCall llm.ContentBlock, index int)
}

// HookCompositor collects multiple handlers per hook and builds a StreamHooks
// with fan-out dispatch. Fan-out hooks fire in registration order.
// Hooks that return a value (OnBeforeToolCall) or have nil-means-disabled
// semantics (OnToolBlockReady) are set directly via Set* methods.
type HookCompositor struct {
	textDelta  []func(string)
	thinking   []func()
	toolStart  []func(string, string, []byte)
	toolEmit   []func(string, string)
	toolResult []func(string, string, string, bool)

	beforeToolCall func(string, string, []byte) (bool, string)
	toolBlockReady func(llm.ContentBlock, int)
}

func (c *HookCompositor) OnTextDelta(fn func(string)) { c.textDelta = append(c.textDelta, fn) }
func (c *HookCompositor) OnThinking(fn func())        { c.thinking = append(c.thinking, fn) }
func (c *HookCompositor) OnToolStart(fn func(string, string, []byte)) {
	c.toolStart = append(c.toolStart, fn)
}
func (c *HookCompositor) OnToolEmit(fn func(string, string)) { c.toolEmit = append(c.toolEmit, fn) }
func (c *HookCompositor) OnToolResult(fn func(string, string, string, bool)) {
	c.toolResult = append(c.toolResult, fn)
}

func (c *HookCompositor) SetBeforeToolCall(fn func(string, string, []byte) (bool, string)) {
	c.beforeToolCall = fn
}

func (c *HookCompositor) SetToolBlockReady(fn func(llm.ContentBlock, int)) {
	c.toolBlockReady = fn
}

// Build returns a StreamHooks where each fan-out hook dispatches to all
// registered handlers in order. Hooks with no registered handlers are nil.
func (c *HookCompositor) Build() StreamHooks {
	var h StreamHooks
	if fns := c.textDelta; len(fns) > 0 {
		h.OnTextDelta = func(text string) {
			for _, fn := range fns {
				fn(text)
			}
		}
	}
	if fns := c.thinking; len(fns) > 0 {
		h.OnThinking = func() {
			for _, fn := range fns {
				fn()
			}
		}
	}
	if fns := c.toolStart; len(fns) > 0 {
		h.OnToolStart = func(name, reason string, input []byte) {
			for _, fn := range fns {
				fn(name, reason, input)
			}
		}
	}
	if fns := c.toolEmit; len(fns) > 0 {
		h.OnToolEmit = func(name, toolUseID string) {
			for _, fn := range fns {
				fn(name, toolUseID)
			}
		}
	}
	if fns := c.toolResult; len(fns) > 0 {
		h.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			for _, fn := range fns {
				fn(name, toolUseID, result, isErr)
			}
		}
	}
	h.OnBeforeToolCall = c.beforeToolCall
	h.OnToolBlockReady = c.toolBlockReady
	return h
}
