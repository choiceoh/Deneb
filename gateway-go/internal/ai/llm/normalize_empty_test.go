package llm

import (
	"encoding/json"
	"testing"
)

func msg(role string, content string) Message {
	return Message{Role: role, Content: json.RawMessage(content)}
}

func TestIsContentEmpty(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"nil content", ``, true},
		{"json null", `null`, true},
		{"empty string", `""`, true},
		{"whitespace string", `"  \n\t"`, true},
		{"empty text block", `[{"type":"text","text":""}]`, true},
		{"whitespace text block", `[{"type":"text","text":"   "}]`, true},
		{"empty block array", `[]`, true},
		{"real text", `"hello"`, false},
		{"text block with text", `[{"type":"text","text":"hi"}]`, false},
		{"tool_use block", `[{"type":"tool_use","id":"t1","name":"x","input":{}}]`, false},
		{"tool_result block", `[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]`, false},
		// Thinking blocks are judged by the wire `thinking` field, not `text`.
		{"empty thinking + empty text (wire-empty boot turn)", `[{"type":"thinking","thinking":""},{"type":"text","text":""}]`, true},
		{"thinking text in text field, empty text block (lost on wire)", `[{"type":"thinking","text":"reasoning only"},{"type":"text"}]`, true},
		{"non-blank thinking field", `[{"type":"thinking","thinking":"real reasoning"}]`, false},
		{"empty thinking + real answer text", `[{"type":"thinking","thinking":""},{"type":"text","text":"이상 없음"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isContentEmpty(json.RawMessage(tc.content)); got != tc.want {
				t.Fatalf("isContentEmpty(%s) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestDropEmptyMessages(t *testing.T) {
	in := []Message{
		msg("user", `"hello"`),
		msg("assistant", `[{"type":"text","text":""}]`), // empty stall artifact
		msg("user", `"are you there?"`),
	}
	out := DropEmptyMessages(in)
	if len(out) != 2 {
		t.Fatalf("DropEmptyMessages kept %d messages, want 2", len(out))
	}
	for _, m := range out {
		if isContentEmpty(m.Content) {
			t.Fatalf("DropEmptyMessages left an empty message: %s", string(m.Content))
		}
	}
	// Input must be untouched.
	if len(in) != 3 {
		t.Fatalf("DropEmptyMessages mutated the input slice (len=%d)", len(in))
	}
}

// An empty assistant message between two user turns must not reach Anthropic
// as an empty message (400 "must not be empty"); after dropping it the two user
// turns are adjacent and must be merged so alternation holds.
func TestDropThenNormalize_EmptyAssistantBetweenUsers(t *testing.T) {
	in := []Message{
		msg("user", `"hi"`),
		msg("assistant", `""`),
		msg("user", `"still there?"`),
	}
	out := NormalizeMessages(DropEmptyMessages(in))
	if len(out) != 1 {
		t.Fatalf("expected the two user turns merged into 1, got %d", len(out))
	}
	if out[0].Role != "user" {
		t.Fatalf("merged message role = %q, want user", out[0].Role)
	}
}

func TestBuildAnthropicRequestBody_DropsEmptyAssistant(t *testing.T) {
	req := ChatRequest{
		Model:     "test-model",
		MaxTokens: 16,
		Messages: []Message{
			msg("user", `"hi"`),
			msg("assistant", `[{"type":"text","text":""}]`),
			msg("user", `"are you there?"`),
		},
	}
	body, err := buildAnthropicRequestBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicRequestBody: %v", err)
	}
	var parsed struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	for i, m := range parsed.Messages {
		if isContentEmpty(m.Content) {
			t.Fatalf("message %d (role %q) reached Anthropic empty: %s", i, m.Role, string(m.Content))
		}
	}
}
