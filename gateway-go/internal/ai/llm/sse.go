package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

const (
	rawSSEBufferSize      = 64
	streamEventBufferSize = 16
)

type sseForwarder func(context.Context, <-chan StreamEvent, chan<- StreamEvent)

// ParseSSE reads server-sent events from r and sends them on the returned
// channel. The channel is closed when r reaches EOF or encounters an error.
//
// SSE format (https://html.spec.whatwg.org/multipage/server-sent-events.html):
//   - Lines starting with ":" are comments (keepalives), ignored.
//   - "event: <type>" sets the event type for the next dispatch.
//   - "data: <payload>" appends to the data buffer.
//   - An empty line dispatches the accumulated event.
//
// Multi-line data fields are joined with "\n".
func ParseSSE(ctx context.Context, r io.Reader) <-chan StreamEvent {
	return parseSSE(ctx, r, 0)
}

// ParseSSEWithByteLimit is ParseSSE with a cumulative wire-byte limit. The
// limit is enforced while scanning, before a delimiter-free sequence of data
// lines can grow dataBuf without bound. A non-positive limit preserves the
// general-purpose parser's historical unlimited behavior.
func ParseSSEWithByteLimit(r io.Reader, maxBytes int) <-chan StreamEvent {
	return parseSSE(context.Background(), r, maxBytes)
}

func parseSSE(ctx context.Context, r io.Reader, maxBytes int) <-chan StreamEvent {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan StreamEvent, rawSSEBufferSize)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		// Allow up to 1 MB per line (LLM responses can be large).
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)

		var eventType string
		var dataBuf strings.Builder
		var wireBytes int

		emit := func(event StreamEvent) bool {
			select {
			case ch <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		emitReadError := func(message string) bool {
			payload, _ := json.Marshal(struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Type: "error", Message: message})
			return emit(StreamEvent{Type: "error", Payload: FlexibleFromRaw(payload)})
		}

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if maxBytes > 0 {
				// Scanner strips the line ending. Count one framing byte per line;
				// this is exact for LF streams and conservatively treats a final
				// unterminated line as if it had a delimiter.
				incoming := len(line) + 1
				if incoming > maxBytes-wireBytes {
					_ = emitReadError("SSE stream read error: configured byte limit exceeded")
					return
				}
				wireBytes += incoming
			}

			// Empty line: dispatch accumulated event.
			if line == "" {
				if dataBuf.Len() > 0 {
					ev := StreamEvent{
						Type:    eventType,
						Payload: FlexibleFromRaw([]byte(dataBuf.String())),
					}
					if !emit(ev) {
						return
					}
				}
				// Reset accumulators.
				eventType = ""
				dataBuf.Reset()
				continue
			}

			// Comment line (keepalive). Deliberately NOT surfaced as an
			// event: the executor's idle watchdog measures real progress
			// (deltas), and comments/pings must not reset it — see the ping
			// drop in forwardAnthropicStream. Long-hold providers that only
			// send comments (the puppet broker) widen/disable the watchdog
			// via DENEB_STREAM_IDLE_TIMEOUT_MS instead (executor_stream.go).
			if strings.HasPrefix(line, ":") {
				continue
			}

			// Parse field.
			field, value, _ := strings.Cut(line, ":")
			// Strip single leading space from value per spec.
			value = strings.TrimPrefix(value, " ")

			switch field {
			case "event":
				eventType = value
			case "data":
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(value)
			}
			// Other fields (id, retry) are ignored.
		}

		// Flush any remaining data (stream ended without trailing blank line).
		if dataBuf.Len() > 0 {
			if !emit(StreamEvent{
				Type:    eventType,
				Payload: FlexibleFromRaw([]byte(dataBuf.String())),
			}) {
				return
			}
		}

		// Surface a scan error (a data line exceeding the 1MB cap → bufio.ErrTooLong,
		// or a mid-stream read failure) as a terminal error event. Without this the
		// goroutine just closes the channel, which is indistinguishable from a clean
		// EOF: the consumer (executor) returns nil on a closed channel with no
		// message_stop and commits the truncated-so-far text as a SUCCESSFUL turn —
		// a user-observed failure silently buried (logging.md). Both provider
		// translators forward a Type=="error" raw event, so the executor surfaces it
		// as "stream error: ..." instead.
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			_ = emitReadError("SSE stream read error: " + err.Error())
		}
	}()
	return ch
}

// startSSEPipelineWithByteLimit owns the common streaming lifecycle shared by
// OpenAI and Anthropic modes: response-body cancellation, raw SSE parsing,
// translation, and deterministic cleanup. A pipeline-local parser context is
// canceled when a translator returns early on a terminal protocol event, so
// buffered trailing provider events cannot strand the parser goroutine.
//
// The byte limit bounds raw provider bytes before an unterminated SSE event can
// grow the parser buffer without limit. A non-positive limit preserves the
// parser's historical unlimited behavior.
func startSSEPipelineWithByteLimit(ctx context.Context, body io.ReadCloser, maxBytes int, forward sseForwarder) <-chan StreamEvent {
	parserCtx, cancelParser := context.WithCancel(ctx)
	rawEvents := parseSSE(parserCtx, body, maxBytes)
	out := make(chan StreamEvent, streamEventBufferSize)

	closeBody := sync.OnceFunc(func() { _ = body.Close() })
	stopContextClose := context.AfterFunc(ctx, closeBody)

	go func() {
		defer close(out)
		defer func() {
			cancelParser()
			stopContextClose()
			closeBody()
			// Joining the parser makes terminal cleanup deterministic. The cancel
			// releases a blocked channel send and Close releases a blocked read.
			for range rawEvents {
			}
		}()
		forward(ctx, rawEvents, out)
	}()

	return out
}
