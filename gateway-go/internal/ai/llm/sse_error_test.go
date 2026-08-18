package llm

import (
	"context"
	"strings"
	"testing"
)

// A data line larger than the scanner's 1MB cap makes bufio.Scanner stop with
// ErrTooLong. ParseSSE must surface that as a terminal error event rather than
// close the channel like a clean EOF — otherwise the consumer commits the
// truncated-so-far stream as a successful turn and the failure vanishes.
func TestParseSSE_OversizedLineSurfacesError(t *testing.T) {
	huge := strings.Repeat("x", 1024*1024+16) // exceeds the 1 MB scanner cap
	input := "data: " + huge + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	events := collectEvents(input)

	sawError := false
	for _, ev := range events {
		if ev.Type == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("oversized data line: want a terminal error event, got %d events without one", len(events))
	}
}

// A well-formed stream must not produce a spurious error event.
func TestParseSSE_CleanStreamHasNoError(t *testing.T) {
	input := "data: {\"a\":1}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, ev := range collectEvents(input) {
		if ev.Type == "error" {
			t.Fatalf("clean stream produced an unexpected error event: %q", ev.Payload.String())
		}
	}
}

func TestParseSSEByteLimitRejectsDelimiterFreeMultilineEvent(t *testing.T) {
	// Every individual line is well below Scanner's 1 MiB cap. Without a
	// cumulative parser limit these lines are retained in dataBuf forever when
	// the provider never sends the blank-line event delimiter.
	input := strings.Repeat("data: 0123456789abcdef\n", 64)
	events := parseSSE(context.Background(), strings.NewReader(input), 128)

	var got []StreamEvent
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 1 {
		t.Fatalf("events = %d, want one terminal error", len(got))
	}
	if got[0].Type != "error" {
		t.Fatalf("event type = %q, want error", got[0].Type)
	}
	if !strings.Contains(got[0].Payload.String(), "byte limit exceeded") {
		t.Fatalf("error payload = %s, want byte-limit message", got[0].Payload)
	}
}
