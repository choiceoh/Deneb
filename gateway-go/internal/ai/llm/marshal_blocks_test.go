package llm

// Regression tests for the empty-Content assistant message bug (2026-06-10):
// step3p7 emitted a tool call whose streamed arguments were non-empty but
// invalid JSON (whitespace-only / truncated). json.Marshal of the block slice
// failed wholesale, NewBlockMessage ignored the error, and the assistant
// message entered the conversation with empty (0-byte) Content — which
// convertMessagesToOpenAI then dropped with a Warn on every later API call.

import (
	"encoding/json"
	"testing"
)

func TestNewBlockMessage_MalformedToolInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"whitespace_only", " "},
		{"newline", "\n"},
		{"truncated_object", `{"path":"fo`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := NewBlockMessage("assistant", []ContentBlock{
				{Type: "text", Text: "calling read"},
				{Type: "tool_use", ID: "toolu_1", Name: "read", Input: FlexibleFromRaw([]byte(tc.input))},
			})

			if msg.Content.Len() == 0 {
				t.Fatal("Content is empty — malformed Input nuked the whole message")
			}
			if !json.Valid(msg.Content.Bytes()) {
				t.Fatalf("Content is not valid JSON: %q", msg.Content)
			}

			var blocks []ContentBlock
			if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
				t.Fatalf("Content does not round-trip to blocks: %v", err)
			}
			if len(blocks) != 2 {
				t.Fatalf("blocks = %d, want 2", len(blocks))
			}
			tu := blocks[1]
			if tu.Type != "tool_use" || tu.ID != "toolu_1" || tu.Name != "read" {
				t.Fatalf("tool_use block mangled: %+v", tu)
			}
			if tu.Input.Len() == 0 || !json.Valid(tu.Input.Bytes()) {
				t.Fatalf("sanitized Input is not valid JSON: %q", tu.Input)
			}
			// The raw fragment must be preserved so the model can see what it
			// actually emitted.
			var wrapped map[string]string
			if err := json.Unmarshal(tu.Input.Bytes(), &wrapped); err != nil {
				t.Fatalf("sanitized Input is not an object: %v", err)
			}
			if wrapped["_malformed_arguments"] != tc.input {
				t.Errorf("_malformed_arguments = %q, want %q", wrapped["_malformed_arguments"], tc.input)
			}
		})
	}
}

func TestNewBlockMessage_EmptyToolInputUnchanged(t *testing.T) {
	// nil/zero-length Input marshals fine via omitempty and must stay as-is
	// (no sanitization wrapper).
	msg := NewBlockMessage("assistant", []ContentBlock{
		{Type: "tool_use", ID: "toolu_1", Name: "read"},
	})
	if !json.Valid(msg.Content.Bytes()) {
		t.Fatalf("Content is not valid JSON: %q", msg.Content)
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil || len(blocks) != 1 {
		t.Fatalf("round-trip failed: err=%v blocks=%d", err, len(blocks))
	}
	if blocks[0].Input.Len() != 0 {
		t.Errorf("empty Input was rewritten: %q", blocks[0].Input)
	}
}

func TestNewBlockMessagePreservesValidToolInput(t *testing.T) {
	in := json.RawMessage(`{"path":"a.go"}`)
	msg := NewBlockMessage("assistant", []ContentBlock{
		{Type: "tool_use", ID: "toolu_1", Name: "read", Input: FlexibleFromRaw(in)},
	})
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if blocks[0].Input.String() != string(in) {
		t.Errorf("valid Input rewritten: got %q, want %q", blocks[0].Input, in)
	}
}

func TestNormalizeMessages_MalformedInputMergeSurvives(t *testing.T) {
	// Two consecutive assistant messages where one carries a malformed Input
	// fragment (possible only via hand-built blocks; factory-built content is
	// already sanitized). mergeContent must not collapse them into a message
	// with empty Content.
	bad := Message{Role: "assistant", Content: marshalBlocks([]ContentBlock{
		{Type: "tool_use", ID: "toolu_1", Name: "read", Input: FlexibleFromRaw([]byte(" "))},
	})}
	good := NewTextMessage("assistant", "and some text")
	merged := NormalizeMessages([]Message{bad, good})
	if len(merged) != 1 {
		t.Fatalf("merged = %d messages, want 1", len(merged))
	}
	if merged[0].Content.Len() == 0 || !json.Valid(merged[0].Content.Bytes()) {
		t.Fatalf("merged Content invalid: %q", merged[0].Content)
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(merged[0].Content.Bytes(), &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("merged round-trip failed: err=%v blocks=%d", err, len(blocks))
	}
}

func TestConvertMessagesToOpenAI_EmptyContentSkippedSilently(t *testing.T) {
	c := NewClient("http://127.0.0.1:0", "")
	msgs := []Message{
		NewTextMessage("user", "hi"),
		{Role: "assistant"}, // 0-byte Content (legacy artifact of the marshal bug)
		NewTextMessage("user", "again"),
	}
	out := c.convertMessagesToOpenAI(msgs, false)
	if len(out) != 2 {
		t.Fatalf("converted = %d messages, want 2 (empty one skipped)", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "user" {
		t.Errorf("unexpected roles: %s, %s", out[0].Role, out[1].Role)
	}
}

func TestConvertMessagesToOpenAI_SanitizedToolUseRoundTrips(t *testing.T) {
	// A factory-built assistant message with malformed args must reach the
	// OpenAI wire as a tool call (not be dropped), with valid JSON arguments.
	c := NewClient("http://127.0.0.1:0", "")
	assistant := NewBlockMessage("assistant", []ContentBlock{
		{Type: "tool_use", ID: "toolu_1", Name: "read", Input: FlexibleFromRaw([]byte(" "))},
	})
	out := c.convertMessagesToOpenAI([]Message{assistant}, false)
	if len(out) != 1 {
		t.Fatalf("converted = %d messages, want 1", len(out))
	}
	if len(out[0].ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(out[0].ToolCalls))
	}
	args := out[0].ToolCalls[0].Function.Arguments
	if !json.Valid([]byte(args)) {
		t.Errorf("wire arguments not valid JSON: %q", args)
	}
}
