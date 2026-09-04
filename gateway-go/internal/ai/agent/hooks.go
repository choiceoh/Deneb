// hooks.go — StreamHooks callbacks for agent streaming events.
package agent

// StreamHooks contains optional callbacks for agent streaming events.
// All fields are optional — nil callbacks are silently skipped.
type StreamHooks struct {
	OnTextDelta func(text string)  // text delta streamed from LLM
	OnThinking  func(delta string) // reasoning/thinking delta received (delta = the reasoning text chunk)
	// OnThinkingBreak fires when a turn is committed, marking the seam the run's
	// assembled thinking text will carry as a blank line (appendRunSection). A
	// live consumer that concatenates the raw deltas needs it to end up with the
	// same shape as AgentResult.Thinking; without it the two texts differ by
	// exactly those separators, and anything matching one against the other
	// (the SSE reasoning translator) falls off its fast path on every
	// multi-turn run.
	OnThinkingBreak func()
	OnToolStart     func(name, reason string, input []byte)          // tool invocation about to execute; reason is thinking text, input is raw JSON args
	OnToolEmit      func(name, toolUseID string, input []byte)       // tool start broadcast (name + ID + raw JSON args for streaming)
	OnToolResult    func(name, toolUseID, result string, isErr bool) // tool result broadcast
	// OnToolProgress fires periodically while a single tool call is still
	// executing (e.g., long-running `exec` or network fetch). Intended to
	// refresh surface liveness indicators (client typing "...") so the
	// channel TTL does not expire during multi-minute tool calls. elapsedSec
	// is the number of seconds since the tool started (never zero — first
	// fire is after at least one tick interval).
	OnToolProgress func(name, toolUseID string, elapsedSec int)
	// OnBeforeToolCall is called before each tool execution. Returns true to
	// block the tool call (with blockReason as the tool output).
	OnBeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)
}

// HookCompositor collects multiple handlers per hook and builds a StreamHooks
// with fan-out dispatch. Fan-out hooks fire in registration order; the
// before-tool-call gates compose first-block-wins in registration order (a
// blocking gate short-circuits the rest).
type HookCompositor struct {
	textDelta    []func(string)
	thinking     []func(string)
	thinkingBrk  []func()
	toolStart    []func(string, string, []byte)
	toolEmit     []func(string, string, []byte)
	toolResult   []func(string, string, string, bool)
	toolProgress []func(string, string, int)

	beforeToolCall []func(string, string, []byte) (bool, string)
}

// OnTextDelta appends a streamed text-delta observer.
func (c *HookCompositor) OnTextDelta(fn func(string)) { c.textDelta = append(c.textDelta, fn) }

// OnThinking appends a streamed reasoning observer.
func (c *HookCompositor) OnThinking(fn func(string)) { c.thinking = append(c.thinking, fn) }

// OnThinkingBreak appends a turn-seam observer for the reasoning stream.
func (c *HookCompositor) OnThinkingBreak(fn func()) { c.thinkingBrk = append(c.thinkingBrk, fn) }

// OnToolStart appends a tool-start observer.
func (c *HookCompositor) OnToolStart(fn func(string, string, []byte)) {
	c.toolStart = append(c.toolStart, fn)
}

// OnToolEmit appends an observer for incremental tool output.
func (c *HookCompositor) OnToolEmit(fn func(string, string, []byte)) {
	c.toolEmit = append(c.toolEmit, fn)
}

// OnToolResult appends a completed tool-call observer.
func (c *HookCompositor) OnToolResult(fn func(string, string, string, bool)) {
	c.toolResult = append(c.toolResult, fn)
}

// OnToolProgress appends a tool progress observer.
func (c *HookCompositor) OnToolProgress(fn func(string, string, int)) {
	c.toolProgress = append(c.toolProgress, fn)
}

// OnBeforeToolCall registers a pre-execution gate. Gates run in registration
// order and the first to block wins — later gates are not consulted for a
// blocked call. This replaced the original single-valued Set slot once a
// second consumer arrived (the untrusted-origin gate composing onto the goal
// loop's idempotency guard) and hand-rolled the chaining at its wiring site.
func (c *HookCompositor) OnBeforeToolCall(fn func(string, string, []byte) (bool, string)) {
	c.beforeToolCall = append(c.beforeToolCall, fn)
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
		h.OnThinking = func(delta string) {
			for _, fn := range fns {
				fn(delta)
			}
		}
	}
	if fns := c.thinkingBrk; len(fns) > 0 {
		h.OnThinkingBreak = func() {
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
		h.OnToolEmit = func(name, toolUseID string, input []byte) {
			for _, fn := range fns {
				fn(name, toolUseID, input)
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
	if fns := c.toolProgress; len(fns) > 0 {
		h.OnToolProgress = func(name, toolUseID string, elapsedSec int) {
			for _, fn := range fns {
				fn(name, toolUseID, elapsedSec)
			}
		}
	}
	if fns := c.beforeToolCall; len(fns) > 0 {
		h.OnBeforeToolCall = func(name, toolCallID string, input []byte) (bool, string) {
			for _, fn := range fns {
				if block, reason := fn(name, toolCallID, input); block {
					return true, reason
				}
			}
			return false, ""
		}
	}
	return h
}
