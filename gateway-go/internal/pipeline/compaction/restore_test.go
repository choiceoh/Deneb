package compaction

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

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
