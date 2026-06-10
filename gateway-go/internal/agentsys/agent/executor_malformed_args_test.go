package agent

// Regression tests for the empty-Content assistant message bug (2026-06-10):
// step3p7 (vLLM OpenAI-compat) emitted a read tool call whose streamed
// arguments were whitespace-only. The tool param parse failed (expected), but
// the turn's assistant message — carrying the tool_use block with the invalid
// Input fragment — failed json.Marshal wholesale and entered the in-memory
// conversation with empty (0-byte) Content. Every later API call in the run
// then dropped it with a Warn: the model never saw its own failed call and
// kept repeating it (observed 19x read loop).

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// assertConversationIntegrity fails the test if any message carries empty or
// invalid-JSON Content, and returns the tool_use blocks found across the
// conversation.
func assertConversationIntegrity(t *testing.T, messages []llm.Message) []llm.ContentBlock {
	t.Helper()
	var toolUses []llm.ContentBlock
	for i, m := range messages {
		if len(m.Content) == 0 {
			t.Fatalf("message %d (role=%s) has empty (0-byte) Content", i, m.Role)
		}
		if !json.Valid(m.Content) {
			t.Fatalf("message %d (role=%s) has invalid JSON Content: %q", i, m.Role, m.Content)
		}
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue // plain text message
		}
		for _, b := range blocks {
			if b.Type == "tool_use" {
				toolUses = append(toolUses, b)
			}
		}
	}
	return toolUses
}

func TestRunAgent_MalformedToolArgs_MessageIntegrity(t *testing.T) {
	tools := newFakeToolExecutor()
	tools.errors["read"] = fmt.Errorf("parse read params: unexpected end of JSON input")

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			// Turn 1: tool call with whitespace-only arguments (the step3p7 shape).
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "read", inputJSON: " "},
			}, 100, 30),
			// Turn 2: model recovers with text.
			buildTextTurnEvents("recovered", 200, 40),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "read it")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}

	toolUses := assertConversationIntegrity(t, result.FinalMessages)
	if len(toolUses) != 1 {
		t.Fatalf("tool_use blocks in conversation = %d, want 1 (failed call must stay in history)", len(toolUses))
	}
	tu := toolUses[0]
	if tu.Name != "read" || tu.ID != "toolu_1" {
		t.Errorf("tool_use mangled: name=%q id=%q", tu.Name, tu.ID)
	}
	if len(tu.Input) == 0 || !json.Valid(tu.Input) {
		t.Errorf("tool_use Input in history is not valid JSON: %q", tu.Input)
	}

	// The tool itself must still have received the raw malformed bytes so its
	// parse error (the model's feedback signal) stays unchanged.
	tools.mu.Lock()
	rawInput := string(tools.calls[0].Input)
	tools.mu.Unlock()
	if rawInput != " " {
		t.Errorf("tool received Input %q, want raw malformed fragment %q", rawInput, " ")
	}
}

func TestRunAgent_EmptyToolArgs_MessageIntegrity(t *testing.T) {
	// Truly empty arguments (no input_json_delta payload) leave Input nil,
	// which always marshaled fine — guard it stays that way.
	tools := newFakeToolExecutor()
	tools.errors["read"] = fmt.Errorf("parse read params: unexpected end of JSON input")

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "read", inputJSON: ""},
			}, 100, 30),
			buildTextTurnEvents("recovered", 200, 40),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "read it")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	toolUses := assertConversationIntegrity(t, result.FinalMessages)
	if len(toolUses) != 1 {
		t.Fatalf("tool_use blocks in conversation = %d, want 1", len(toolUses))
	}
}
