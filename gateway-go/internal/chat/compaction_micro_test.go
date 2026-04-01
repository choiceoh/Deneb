package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func makeToolResultMessage(toolUseID, content string) llm.Message {
	blocks := []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: toolUseID, Content: content},
	}
	raw, _ := json.Marshal(blocks)
	return llm.Message{Role: "user", Content: raw}
}

func makeTextMessage(role, text string) llm.Message {
	return llm.NewTextMessage(role, text)
}

func TestMicrocompactMessages(t *testing.T) {
	now := time.Now()
	staleTime := now.Add(-5 * time.Minute) // well past threshold
	_ = staleTime

	t.Run("empty messages", func(t *testing.T) {
		msgs, result := MicrocompactMessages(nil, now)
		if len(msgs) != 0 {
			t.Error("expected empty")
		}
		if result.Reason != "no_messages" {
			t.Errorf("reason = %q", result.Reason)
		}
	})

	t.Run("no tool results", func(t *testing.T) {
		msgs := []llm.Message{
			makeTextMessage("user", "hello"),
			makeTextMessage("assistant", "hi there"),
		}
		_, result := MicrocompactMessages(msgs, now)
		if result.Reason != "no_tool_results" {
			t.Errorf("reason = %q", result.Reason)
		}
	})

	t.Run("too few to prune", func(t *testing.T) {
		// Use a "stale" time far in the future so staleness check passes.
		stale := now.Add(10 * time.Minute)
		msgs := []llm.Message{
			makeTextMessage("assistant", "let me check"),
		}
		// Add fewer than microcompactKeepRecent tool results.
		for i := 0; i < microcompactKeepRecent; i++ {
			msgs = append(msgs, makeToolResultMessage("id-"+string(rune('a'+i)), "result content"))
		}
		_, result := MicrocompactMessages(msgs, stale)
		if result.Reason != "below_keep_threshold" {
			t.Errorf("reason = %q, want below_keep_threshold", result.Reason)
		}
	})

	t.Run("prunes old tool results", func(t *testing.T) {
		// Use a "stale" time far in the future so the staleness check passes.
		stale := now.Add(10 * time.Minute)
		msgs := []llm.Message{
			makeTextMessage("assistant", "initial response"),
		}
		// Add more than microcompactKeepRecent tool results.
		total := microcompactKeepRecent + 5
		for i := 0; i < total; i++ {
			content := strings.Repeat("x", 1000) // substantial content
			msgs = append(msgs, makeToolResultMessage("id-"+string(rune('a'+i)), content))
		}
		// Add another assistant message at the end (stale scenario).
		msgs = append(msgs, makeTextMessage("assistant", "done"))

		pruned, result := MicrocompactMessages(msgs, stale)
		if result.Reason != "pruned" {
			t.Errorf("reason = %q, want pruned", result.Reason)
		}
		if result.PrunedCount != 5 {
			t.Errorf("prunedCount = %d, want 5", result.PrunedCount)
		}
		if result.EstimatedSaved <= 0 {
			t.Error("expected positive EstimatedSaved")
		}
		if len(pruned) != len(msgs) {
			t.Error("message count should not change")
		}

		// Verify first 5 tool results are pruned.
		for i := 1; i <= 5; i++ {
			var blocks []llm.ContentBlock
			if err := json.Unmarshal(pruned[i].Content, &blocks); err != nil {
				t.Fatalf("unmarshal blocks: %v", err)
			}
			if !strings.Contains(blocks[0].Content, "pruned") {
				t.Errorf("message %d should be pruned, got %q", i, blocks[0].Content)
			}
		}

		// Verify last 8 tool results are preserved.
		for i := 6; i <= total; i++ {
			var blocks []llm.ContentBlock
			if err := json.Unmarshal(pruned[i].Content, &blocks); err != nil {
				t.Fatalf("unmarshal blocks: %v", err)
			}
			if strings.Contains(blocks[0].Content, "pruned") {
				t.Errorf("message %d should be preserved", i)
			}
		}
	})

	t.Run("does not mutate original", func(t *testing.T) {
		msgs := []llm.Message{
			makeTextMessage("assistant", "response"),
		}
		for i := 0; i < microcompactKeepRecent+3; i++ {
			msgs = append(msgs, makeToolResultMessage("id", strings.Repeat("x", 500)))
		}
		msgs = append(msgs, makeTextMessage("assistant", "done"))

		original := make([]llm.Message, len(msgs))
		copy(original, msgs)

		MicrocompactMessages(msgs, now)

		// Original should be unchanged.
		for i := range msgs {
			if string(msgs[i].Content) != string(original[i].Content) {
				t.Errorf("original message %d was mutated", i)
			}
		}
	})
}
