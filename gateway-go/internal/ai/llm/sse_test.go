package llm

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func collectEvents(input string) []StreamEvent {
	ch := ParseSSE(context.Background(), strings.NewReader(input))
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestParseSSE_BasicEvent(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("got %d, want 1 event", len(events))
	}
	if events[0].Type != "message_start" {
		t.Errorf("type = %q, want %q", events[0].Type, "message_start")
	}
	if events[0].Payload.String() != `{"type":"message_start"}` {
		t.Errorf("payload = %q", events[0].Payload.String())
	}
}

func TestParseSSE_MultipleEvents(t *testing.T) {
	input := "event: a\ndata: {\"n\":1}\n\nevent: b\ndata: {\"n\":2}\n\n"
	events := collectEvents(input)

	if len(events) != 2 {
		t.Fatalf("got %d, want 2 events", len(events))
	}
	if events[0].Type != "a" {
		t.Errorf("events[0].Type = %q", events[0].Type)
	}
	if events[1].Type != "b" {
		t.Errorf("events[1].Type = %q", events[1].Type)
	}
}

func TestParseSSE_MultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("got %d, want 1 event", len(events))
	}
	if events[0].Payload.String() != "line1\nline2" {
		t.Errorf("payload = %q, want %q", events[0].Payload.String(), "line1\nline2")
	}
}

func TestParseSSE_CommentIgnored(t *testing.T) {
	input := ": keepalive\nevent: ping\ndata: {}\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("got %d, want 1 event", len(events))
	}
	if events[0].Type != "ping" {
		t.Errorf("type = %q", events[0].Type)
	}
}

func TestParseSSE_NoTrailingBlankLine(t *testing.T) {
	// Stream ends without final blank line — should still flush.
	input := "event: final\ndata: {\"done\":true}"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("got %d, want 1 event", len(events))
	}
	if events[0].Type != "final" {
		t.Errorf("type = %q", events[0].Type)
	}
}

func TestParseSSE_DataWithColon(t *testing.T) {
	// Data containing colons should be preserved.
	input := "data: {\"url\":\"https://example.com\"}\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("got %d, want 1 event", len(events))
	}
	expected := `{"url":"https://example.com"}`
	if events[0].Payload.String() != expected {
		t.Errorf("payload = %q, want %q", events[0].Payload.String(), expected)
	}
}

func TestParseSSE_CancelUnblocksSaturatedOutput(t *testing.T) {
	var input strings.Builder
	for i := range rawSSEBufferSize + 32 {
		fmt.Fprintf(&input, "event: chunk\ndata: {\"index\":%d}\n\n", i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := ParseSSE(ctx, strings.NewReader(input.String()))
	waitForSSEBufferLen(t, events, rawSSEBufferSize)

	// The parser is now blocked trying to send event 65. Cancellation must
	// release that send even though no consumer made room in the channel.
	cancel()
	drained := make(chan int, 1)
	go func() {
		count := 0
		for range events {
			count++
		}
		drained <- count
	}()

	select {
	case count := <-drained:
		if count != rawSSEBufferSize {
			t.Fatalf("drained %d events after saturation cancel, want %d buffered events", count, rawSSEBufferSize)
		}
	case <-time.After(time.Second):
		t.Fatal("saturated SSE parser did not exit after cancellation")
	}
}

type countingReadCloser struct {
	io.Reader
	closes atomic.Int32
}

func (r *countingReadCloser) Close() error {
	r.closes.Add(1)
	return nil
}

func TestStartSSEPipelineWithByteLimit_EarlyTerminalCleansParserAndClosesBodyOnce(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	for i := range rawSSEBufferSize + 32 {
		fmt.Fprintf(&input, "event: trailing\ndata: {\"index\":%d}\n\n", i)
	}
	body := &countingReadCloser{Reader: strings.NewReader(input.String())}

	ctx, cancel := context.WithCancel(context.Background())
	rawCaptured := make(chan (<-chan StreamEvent), 1)
	terminalSeen := make(chan struct{})
	releaseForwarder := make(chan struct{})
	out := startSSEPipelineWithByteLimit(ctx, body, 0, func(_ context.Context, raw <-chan StreamEvent, _ chan<- StreamEvent) {
		rawCaptured <- raw
		terminal := <-raw
		if terminal.Type != "message_stop" {
			t.Errorf("first event type = %q, want message_stop", terminal.Type)
		}
		close(terminalSeen)
		<-releaseForwarder
	})

	raw := <-rawCaptured
	<-terminalSeen
	waitForSSEBufferLen(t, raw, rawSSEBufferSize)

	// Exercise both close paths concurrently: context cancellation closes the
	// response body, then terminal cleanup cancels and joins the saturated
	// parser. sync.Once must keep Close exact-once.
	cancel()
	close(releaseForwarder)

	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("pipeline unexpectedly emitted an output event")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal pipeline cleanup did not finish")
	}
	if got := body.closes.Load(); got != 1 {
		t.Fatalf("response body Close calls = %d, want exactly 1", got)
	}
}

func waitForSSEBufferLen(t *testing.T, events <-chan StreamEvent, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(events) != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(events); got != want {
		t.Fatalf("SSE buffer length = %d, want %d", got, want)
	}
}
