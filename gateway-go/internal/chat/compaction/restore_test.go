package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestExtractRecentFileReads(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		records := ExtractRecentFileReads(nil)
		if len(records) != 0 {
			t.Errorf("expected 0 records, got %d", len(records))
		}
	})

	t.Run("extracts tool results", func(t *testing.T) {
		msgs := []llm.Message{
			makeTextMessage("assistant", "let me read the file"),
			makeToolResultMessage("read-1", "package main\nfunc main() {}"),
			makeTextMessage("assistant", "and another"),
			makeToolResultMessage("read-2", "package foo\nfunc Bar() {}"),
		}
		records := ExtractRecentFileReads(msgs)
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
		// Most recent first.
		if records[0].Path != "read-2" {
			t.Errorf("first record should be most recent, got %q", records[0].Path)
		}
	})

	t.Run("deduplicates by path", func(t *testing.T) {
		msgs := []llm.Message{
			makeToolResultMessage("file-a", "old content"),
			makeTextMessage("assistant", "editing"),
			makeToolResultMessage("file-a", "new content"),
		}
		records := ExtractRecentFileReads(msgs)
		if len(records) != 1 {
			t.Fatalf("expected 1 record after dedup, got %d", len(records))
		}
		if records[0].Content != "new content" {
			t.Errorf("should keep most recent content, got %q", records[0].Content)
		}
	})
}

func TestBuildRestorationMessages(t *testing.T) {
	t.Run("empty records", func(t *testing.T) {
		msgs := BuildRestorationMessages(nil)
		if len(msgs) != 0 {
			t.Error("expected nil")
		}
	})

	t.Run("builds messages within budget", func(t *testing.T) {
		records := []FileReadRecord{
			{Path: "a.go", Content: "package a", TokenCount: 100, TurnIndex: 2},
			{Path: "b.go", Content: "package b", TokenCount: 100, TurnIndex: 1},
		}
		msgs := BuildRestorationMessages(records)
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
	})

	t.Run("respects max files", func(t *testing.T) {
		records := make([]FileReadRecord, postCompactMaxFiles+5)
		for i := range records {
			records[i] = FileReadRecord{
				Path: "file", Content: "content", TokenCount: 10, TurnIndex: i,
			}
		}
		msgs := BuildRestorationMessages(records)
		if len(msgs) > postCompactMaxFiles {
			t.Errorf("expected <= %d messages, got %d", postCompactMaxFiles, len(msgs))
		}
	})
}

func TestStripImageBlocks(t *testing.T) {
	t.Run("no images unchanged", func(t *testing.T) {
		msgs := []llm.Message{
			makeTextMessage("user", "hello"),
			makeTextMessage("assistant", "hi"),
		}
		stripped := StripImageBlocks(msgs)
		if len(stripped) != 2 {
			t.Fatalf("expected 2 messages")
		}
	})

	t.Run("strips image blocks", func(t *testing.T) {
		blocks := []llm.ContentBlock{
			{Type: "text", Text: "see this image:"},
			{Type: "image", Source: &llm.ImageSource{Type: "base64", MediaType: "image/png", Data: "abc123"}},
		}
		raw, _ := json.Marshal(blocks)
		msgs := []llm.Message{
			{Role: "user", Content: raw},
		}
		stripped := StripImageBlocks(msgs)

		var resultBlocks []llm.ContentBlock
		if err := json.Unmarshal(stripped[0].Content, &resultBlocks); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resultBlocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(resultBlocks))
		}
		if resultBlocks[0].Type != "text" || resultBlocks[0].Text != "see this image:" {
			t.Error("first block should be preserved")
		}
		if resultBlocks[1].Type != "text" || !strings.Contains(resultBlocks[1].Text, "removed") {
			t.Errorf("second block should be replacement stub, got %+v", resultBlocks[1])
		}
	})
}
