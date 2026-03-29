// async_hooks.go — Zen arch: Superscalar Unit Separation for hook dispatch.
//
// CPU analogy: In a superscalar processor, the instruction parser (frontend)
// and execution units (backend) run independently. The parser feeds decoded
// instructions into a queue; execution units drain it asynchronously.
//
// Problem: consumeStream's hook callbacks (OnTextDelta, OnThinking, etc.)
// execute synchronously in the stream parsing loop. If a hook chain includes
// slow operations (Discord API calls ~10-20ms, typing indicators ~1ms), the
// LLM stream parsing stalls — wasting time that could be spent accumulating
// the next token.
//
// Solution: AsyncHookDispatcher wraps StreamHooks with a bounded queue.
// The stream parser enqueues hook events without blocking; a dedicated
// goroutine drains the queue and calls the actual hooks sequentially
// (preserving event ordering within each hook type).
//
// Ordering guarantee: events are dispatched in FIFO order within the queue,
// so OnTextDelta("hello") always fires before OnTextDelta("world") if
// enqueued in that order.
package agent

// hookEvent represents a deferred hook invocation.
type hookEvent struct {
	kind    hookKind
	text    string // for textDelta, toolName, toolUseID
	text2   string // for toolStart reason, toolResult result
	isError bool   // for toolResult
}

type hookKind uint8

const (
	hookTextDelta hookKind = iota
	hookThinking
	hookToolStart
	hookToolEmit
	hookToolResult
)

// AsyncHookDispatcher decouples hook dispatch from the stream parsing loop.
// Create with NewAsyncHookDispatcher, use the returned StreamHooks in
// consumeStream, and call Close() when the turn/run ends.
type AsyncHookDispatcher struct {
	queue chan hookEvent
	inner StreamHooks
	done  chan struct{}
}

// asyncHookQueueSize is the buffered channel capacity. Sized to absorb bursts
// of streaming deltas without blocking the parser. At 50 tokens/sec with 20ms
// hooks, we need ~1 token of buffer — 64 provides generous headroom.
const asyncHookQueueSize = 64

// NewAsyncHookDispatcher wraps the given hooks with an async dispatch queue.
// The returned StreamHooks should be used in place of the originals.
// Call Close() when the run is done to drain remaining events.
func NewAsyncHookDispatcher(inner StreamHooks) (*AsyncHookDispatcher, StreamHooks) {
	d := &AsyncHookDispatcher{
		queue: make(chan hookEvent, asyncHookQueueSize),
		inner: inner,
		done:  make(chan struct{}),
	}

	go d.drain()

	// Build a StreamHooks that enqueues instead of calling directly.
	proxy := StreamHooks{}

	if inner.OnTextDelta != nil {
		proxy.OnTextDelta = func(text string) {
			select {
			case d.queue <- hookEvent{kind: hookTextDelta, text: text}:
			default:
				// Queue full — call synchronously to avoid dropping events.
				inner.OnTextDelta(text)
			}
		}
	}
	if inner.OnThinking != nil {
		proxy.OnThinking = func() {
			select {
			case d.queue <- hookEvent{kind: hookThinking}:
			default:
				inner.OnThinking()
			}
		}
	}
	if inner.OnToolStart != nil {
		proxy.OnToolStart = func(name, reason string) {
			select {
			case d.queue <- hookEvent{kind: hookToolStart, text: name, text2: reason}:
			default:
				inner.OnToolStart(name, reason)
			}
		}
	}
	if inner.OnToolEmit != nil {
		proxy.OnToolEmit = func(name, toolUseID string) {
			select {
			case d.queue <- hookEvent{kind: hookToolEmit, text: name, text2: toolUseID}:
			default:
				inner.OnToolEmit(name, toolUseID)
			}
		}
	}
	if inner.OnToolResult != nil {
		proxy.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			select {
			case d.queue <- hookEvent{kind: hookToolResult, text: name, text2: toolUseID + "\x00" + result, isError: isErr}:
			default:
				inner.OnToolResult(name, toolUseID, result, isErr)
			}
		}
	}

	return d, proxy
}

// drain processes hook events sequentially from the queue.
// Runs in a dedicated goroutine — the "execution unit" of the superscalar design.
func (d *AsyncHookDispatcher) drain() {
	defer close(d.done)
	for ev := range d.queue {
		switch ev.kind {
		case hookTextDelta:
			if d.inner.OnTextDelta != nil {
				d.inner.OnTextDelta(ev.text)
			}
		case hookThinking:
			if d.inner.OnThinking != nil {
				d.inner.OnThinking()
			}
		case hookToolStart:
			if d.inner.OnToolStart != nil {
				d.inner.OnToolStart(ev.text, ev.text2)
			}
		case hookToolEmit:
			if d.inner.OnToolEmit != nil {
				d.inner.OnToolEmit(ev.text, ev.text2)
			}
		case hookToolResult:
			if d.inner.OnToolResult != nil {
				// Split toolUseID and result back apart.
				parts := splitOnce(ev.text2, '\x00')
				d.inner.OnToolResult(ev.text, parts[0], parts[1], ev.isError)
			}
		}
	}
}

// Close signals no more events and waits for the drain goroutine to finish
// processing all queued events. Must be called once per run.
func (d *AsyncHookDispatcher) Close() {
	close(d.queue)
	<-d.done
}

// splitOnce splits s into two parts at the first occurrence of sep.
func splitOnce(s string, sep byte) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}
