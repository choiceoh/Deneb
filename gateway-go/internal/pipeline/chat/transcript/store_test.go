package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

var NewTextChatMessage = toolctx.NewTextChatMessage

func TestFileTranscriptStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	msg1 := NewTextChatMessage("user", "hello", 0)
	msg2 := NewTextChatMessage("assistant", "hi there", 0)

	if err := store.Append("test-session", msg1); err != nil {
		t.Fatalf("Append msg1: %v", err)
	}
	if err := store.Append("test-session", msg2); err != nil {
		t.Fatalf("Append msg2: %v", err)
	}

	msgs, total, err := store.Load("test-session", 0)
	testutil.NoError(t, err)
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].TextContent() != "hello" {
		t.Errorf("msgs[0].Content = %q", msgs[0].TextContent())
	}
	if msgs[1].TextContent() != "hi there" {
		t.Errorf("msgs[1].Content = %q", msgs[1].TextContent())
	}
}

func TestFileTranscriptStore_LoadWithLimit(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	for range 5 {
		store.Append("session", NewTextChatMessage("user", "msg", 0))
	}

	msgs, total, err := store.Load("session", 2)
	testutil.NoError(t, err)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
}

func TestFileTranscriptStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	store := NewFileTranscriptStore(dir)

	err := store.Append("session", NewTextChatMessage("user", "test", 0))
	testutil.NoError(t, err)

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, "session.jsonl")); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

// TestFileTranscriptStore_RedactsLegacyText covers the common path where
// ChatMessage.Content is a plain JSON string (legacy format).
func TestFileTranscriptStore_RedactsLegacyText(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	token := "sk-ant-" + strings.Repeat("Z", 40) // synthetic, not a real credential
	msg := NewTextChatMessage("tool", "Contents of .env: ANTHROPIC_API_KEY="+token, 0)
	if err := store.Append("sess", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	path := filepath.Join(dir, "sess.jsonl")
	data := testutil.Must(os.ReadFile(path))
	if strings.Contains(string(data), token) {
		t.Fatalf("persisted transcript still contains raw token: %q", string(data))
	}
	if !strings.Contains(string(data), `"role":"tool"`) {
		t.Errorf("role field lost after redaction: %q", string(data))
	}
}

// TestFileTranscriptStore_RedactsRichBlocks covers the ContentBlock array
// shape used by newer assistant messages with tool_use / tool_result blocks.
func TestFileTranscriptStore_RedactsRichBlocks(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	token := "ghp_" + strings.Repeat("Z", 36)
	blocks := []map[string]any{
		{"type": "text", "text": "here is the token " + token},
		{"type": "tool_use", "id": "tu_1", "name": "fs_read", "input": map[string]any{"path": ".env"}},
	}
	raw, _ := json.Marshal(blocks)
	msg := toolctx.ChatMessage{
		Role:    "assistant",
		Content: raw,
	}
	if err := store.Append("rich", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data := testutil.Must(os.ReadFile(filepath.Join(dir, "rich.jsonl")))
	body := string(data)
	if strings.Contains(body, token) {
		t.Fatalf("rich-format transcript still contains raw token: %q", body)
	}
	// Structural fields remain.
	if !strings.Contains(body, `"name":"fs_read"`) {
		t.Errorf("tool_use name lost: %q", body)
	}
	if !strings.Contains(body, `"id":"tu_1"`) {
		t.Errorf("tool_use id lost: %q", body)
	}
}

// TestFileTranscriptStore_PreservesKorean ensures Korean text is unaffected.
func TestFileTranscriptStore_PreservesKorean(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	const korean = "오늘 회의록을 정리해줄래?"
	msg := NewTextChatMessage("user", korean, 0)
	if err := store.Append("ko", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data := testutil.Must(os.ReadFile(filepath.Join(dir, "ko.jsonl")))
	if !strings.Contains(string(data), korean) {
		t.Fatalf("Korean text was mangled: %q", string(data))
	}
}

func TestFileTranscriptStore_ConcurrentAppendKeepsEveryJSONRecord(t *testing.T) {
	store := NewFileTranscriptStore(t.TempDir())
	const writers = 64
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.Append("shared", NewTextChatMessage("user", fmt.Sprintf("message-%02d", i), 0)); err != nil {
				t.Errorf("Append(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	msgs, total, err := store.Load("shared", 0)
	testutil.NoError(t, err)
	if total != writers || len(msgs) != writers {
		t.Fatalf("concurrent Load = %d messages (total %d), want %d", len(msgs), total, writers)
	}
	seen := make(map[string]bool, writers)
	for _, msg := range msgs {
		seen[msg.TextContent()] = true
	}
	for i := range writers {
		want := fmt.Sprintf("message-%02d", i)
		if !seen[want] {
			t.Errorf("missing concurrent record %q", want)
		}
	}
}

func TestMemoryTranscriptStore_SearchHonorsLimitAndSessionBoundaries(t *testing.T) {
	store := NewMemoryTranscriptStore()
	for _, tc := range []struct {
		key, text string
	}{
		{"client:a", "Alpha project kickoff"},
		{"client:a", "unrelated note"},
		{"client:b", "ALPHA risk review"},
		{"client:c", "alpha delivery"},
	} {
		if err := store.Append(tc.key, NewTextChatMessage("user", tc.text, 0)); err != nil {
			t.Fatalf("Append(%s): %v", tc.key, err)
		}
	}

	results, err := store.Search("alpha", 2)
	testutil.NoError(t, err)
	if len(results) != 2 {
		t.Fatalf("Search result sessions = %d, want global limit 2", len(results))
	}
	matches := 0
	for _, result := range results {
		for _, match := range result.Matches {
			matches++
			if !strings.Contains(strings.ToLower(match.Message.TextContent()), "alpha") {
				t.Errorf("non-matching result: %#v", match)
			}
		}
	}
	if matches != 2 {
		t.Fatalf("Search matches = %d, want 2", matches)
	}
}
