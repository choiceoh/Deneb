// hooks.go — StreamHooks callbacks for agent streaming events.
package agent

import "github.com/choiceoh/deneb/gateway-go/internal/llm"

// StreamHooks contains optional callbacks for agent streaming events.
// All fields are optional — nil callbacks are silently skipped.
type StreamHooks struct {
	OnTextDelta  func(text string)                              // text delta streamed from LLM
	OnThinking   func()                                        // reasoning/thinking delta received
	OnToolStart  func(name, reason string)                     // tool invocation about to execute; reason is a brief thinking summary
	OnToolEmit   func(name, toolUseID string)                  // tool start broadcast (name + ID for streaming)
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
