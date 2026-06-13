package compaction

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestStripThinkingBlocks(t *testing.T) {
	mustBlocks := func(blocks ...llm.ContentBlock) llm.Message {
		raw, err := json.Marshal(blocks)
		if err != nil {
			t.Fatalf("marshal blocks: %v", err)
		}
		return llm.Message{Role: "assistant", Content: raw}
	}
	blockTypes := func(t *testing.T, msg llm.Message) []string {
		t.Helper()
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			t.Fatalf("unmarshal content %q: %v", msg.Content, err)
		}
		types := make([]string, len(blocks))
		for i, b := range blocks {
			types[i] = b.Type
		}
		return types
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("drops thinking but keeps text and tool_use", func(t *testing.T) {
		in := []llm.Message{
			llm.NewTextMessage("user", "hi"),
			mustBlocks(
				llm.ContentBlock{Type: "thinking", Thinking: "secret reasoning", Signature: "sig-abc"},
				llm.ContentBlock{Type: "text", Text: "the answer"},
				llm.ContentBlock{Type: "tool_use", ID: "t1", Name: "exec", Input: json.RawMessage(`{}`)},
			),
		}
		out, n := StripThinkingBlocks(in)
		if n != 1 {
			t.Fatalf("stripped count = %d, want 1", n)
		}
		// User message (string content) is untouched.
		if string(out[0].Content) != string(in[0].Content) {
			t.Errorf("user message changed: %q", out[0].Content)
		}
		if got := blockTypes(t, out[1]); !eq(got, []string{"text", "tool_use"}) {
			t.Errorf("assistant block types = %v, want [text tool_use]", got)
		}
	})

	t.Run("drops redacted_thinking", func(t *testing.T) {
		in := []llm.Message{mustBlocks(
			llm.ContentBlock{Type: "redacted_thinking", Data: "EncryptedOpaque=="},
			llm.ContentBlock{Type: "text", Text: "answer"},
		)}
		out, n := StripThinkingBlocks(in)
		if n != 1 {
			t.Fatalf("stripped count = %d, want 1", n)
		}
		if got := blockTypes(t, out[0]); !eq(got, []string{"text"}) {
			t.Errorf("block types = %v, want [text]", got)
		}
	})

	t.Run("thinking-only assistant becomes empty block array", func(t *testing.T) {
		in := []llm.Message{mustBlocks(
			llm.ContentBlock{Type: "thinking", Thinking: "only reasoning", Signature: "sig"},
		)}
		out, n := StripThinkingBlocks(in)
		if n != 1 {
			t.Fatalf("stripped count = %d, want 1", n)
		}
		if got := blockTypes(t, out[0]); len(got) != 0 {
			t.Errorf("block types = %v, want empty", got)
		}
	})

	t.Run("no thinking blocks: unchanged, count 0", func(t *testing.T) {
		in := []llm.Message{
			llm.NewTextMessage("user", "plain string content"),
			mustBlocks(llm.ContentBlock{Type: "text", Text: "no reasoning here"}),
		}
		out, n := StripThinkingBlocks(in)
		if n != 0 {
			t.Fatalf("stripped count = %d, want 0", n)
		}
		for i := range in {
			if string(out[i].Content) != string(in[i].Content) {
				t.Errorf("message %d changed: %q -> %q", i, in[i].Content, out[i].Content)
			}
		}
	})

	t.Run("counts across multiple messages", func(t *testing.T) {
		in := []llm.Message{
			mustBlocks(
				llm.ContentBlock{Type: "thinking", Thinking: "a"},
				llm.ContentBlock{Type: "text", Text: "x"},
			),
			mustBlocks(
				llm.ContentBlock{Type: "redacted_thinking", Data: "d"},
				llm.ContentBlock{Type: "thinking", Thinking: "b"},
				llm.ContentBlock{Type: "text", Text: "y"},
			),
		}
		if _, n := StripThinkingBlocks(in); n != 3 {
			t.Fatalf("stripped count = %d, want 3", n)
		}
	})
}

// assistantToolUseMsg builds a minimal assistant message that invokes a single
// file-reading tool with the given id and path.
func assistantToolUseMsg(t *testing.T, toolName, id, path string) llm.Message {
	t.Helper()
	input, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	blocks := []llm.ContentBlock{{
		Type:  "tool_use",
		ID:    id,
		Name:  toolName,
		Input: input,
	}}
	return llm.NewBlockMessage("assistant", blocks)
}

// userToolResultMsg builds a minimal user message that carries a tool_result
// for a prior tool_use id.
func userToolResultMsg(t *testing.T, toolUseID, content string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}}
	return llm.NewBlockMessage("user", blocks)
}

// TestExtractRecentFileReads_ReReadReordersToFront covers the dedup+ordering
// invariant: when a file is re-read later in the conversation, the extractor
// must treat that second read as the most recent — i.e. the returned slice
// lists it at index 0, not at whatever position it first appeared.
func TestExtractRecentFileReads_ReReadReordersToFront(t *testing.T) {
	msgs := []llm.Message{
		assistantToolUseMsg(t, "read_file", "t1", "A.go"),
		userToolResultMsg(t, "t1", "A v1"),
		assistantToolUseMsg(t, "read_file", "t2", "B.go"),
		userToolResultMsg(t, "t2", "B"),
		assistantToolUseMsg(t, "read_file", "t3", "A.go"),
		userToolResultMsg(t, "t3", "A v2"),
	}

	records := ExtractRecentFileReads(msgs)
	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d", len(records))
	}
	if records[0].Path != "A.go" {
		t.Errorf("records[0] should be A.go (most recent), got %s", records[0].Path)
	}
	if records[0].Content != "A v2" {
		t.Errorf("records[0] should carry latest content 'A v2', got %q", records[0].Content)
	}
	if records[1].Path != "B.go" {
		t.Errorf("records[1] should be B.go, got %s", records[1].Path)
	}
}

// TestExtractRecentFileReads_Order covers the plain (no re-read) case so we
// detect any regression that swaps the reverse-at-end step.
func TestExtractRecentFileReads_Order(t *testing.T) {
	msgs := []llm.Message{
		assistantToolUseMsg(t, "read_file", "t1", "A.go"),
		userToolResultMsg(t, "t1", "A"),
		assistantToolUseMsg(t, "read_file", "t2", "B.go"),
		userToolResultMsg(t, "t2", "B"),
		assistantToolUseMsg(t, "read_file", "t3", "C.go"),
		userToolResultMsg(t, "t3", "C"),
	}

	records := ExtractRecentFileReads(msgs)
	if len(records) != 3 {
		t.Fatalf("want 3 records, got %d", len(records))
	}
	wantPaths := []string{"C.go", "B.go", "A.go"}
	for i, want := range wantPaths {
		if records[i].Path != want {
			t.Errorf("records[%d] = %s, want %s", i, records[i].Path, want)
		}
	}
}

// TestExtractRecentFileReads_IgnoresErrors ensures failed tool_results are not
// re-injected as "file content" during restoration.
func TestExtractRecentFileReads_IgnoresErrors(t *testing.T) {
	errBlocks := []llm.ContentBlock{{
		Type:      "tool_result",
		ToolUseID: "t1",
		Content:   "enoent",
		IsError:   true,
	}}
	msgs := []llm.Message{
		assistantToolUseMsg(t, "read_file", "t1", "A.go"),
		llm.NewBlockMessage("user", errBlocks),
	}

	records := ExtractRecentFileReads(msgs)
	if len(records) != 0 {
		t.Fatalf("error result should be skipped, got %d records", len(records))
	}
}
