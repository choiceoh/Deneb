package llm

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

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
func ParseSSE(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		// Allow up to 1 MB per line (LLM responses can be large).
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)

		var eventType string
		var dataBuf strings.Builder

		for scanner.Scan() {
			line := scanner.Text()

			// Empty line: dispatch accumulated event.
			if line == "" {
				if dataBuf.Len() > 0 {
					ev := StreamEvent{
						Type:    eventType,
						Payload: json.RawMessage(dataBuf.String()),
					}
					ch <- ev
				}
				// Reset accumulators.
				eventType = ""
				dataBuf.Reset()
				continue
			}

			// Comment line (keepalive).
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
			ch <- StreamEvent{
				Type:    eventType,
				Payload: json.RawMessage(dataBuf.String()),
			}
		}
	}()
	return ch
}
