package llm

import (
	"strings"
	"testing"
)

func collectEvents(input string) []StreamEvent {
	ch := ParseSSE(strings.NewReader(input))
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
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "message_start" {
		t.Errorf("type = %q, want %q", events[0].Type, "message_start")
	}
	if string(events[0].Payload) != `{"type":"message_start"}` {
		t.Errorf("payload = %q", string(events[0].Payload))
	}
}

func TestParseSSE_MultipleEvents(t *testing.T) {
	input := "event: a\ndata: {\"n\":1}\n\nevent: b\ndata: {\"n\":2}\n\n"
	events := collectEvents(input)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
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
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if string(events[0].Payload) != "line1\nline2" {
		t.Errorf("payload = %q, want %q", string(events[0].Payload), "line1\nline2")
	}
}

func TestParseSSE_CommentIgnored(t *testing.T) {
	input := ": keepalive\nevent: ping\ndata: {}\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "ping" {
		t.Errorf("type = %q", events[0].Type)
	}
}

func TestParseSSE_EmptyData(t *testing.T) {
	// Only comments and blank lines — no events dispatched.
	input := ": comment\n\n: another\n\n"
	events := collectEvents(input)

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestParseSSE_NoTrailingBlankLine(t *testing.T) {
	// Stream ends without final blank line — should still flush.
	input := "event: final\ndata: {\"done\":true}"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
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
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	expected := `{"url":"https://example.com"}`
	if string(events[0].Payload) != expected {
		t.Errorf("payload = %q, want %q", string(events[0].Payload), expected)
	}
}

func TestParseSSE_NoEventField(t *testing.T) {
	// Event type defaults to empty string when no "event:" line.
	input := "data: {\"hello\":true}\n\n"
	events := collectEvents(input)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "" {
		t.Errorf("type = %q, want empty", events[0].Type)
	}
}
