package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadTopicKnowledge_EmptyKey(t *testing.T) {
	got := LoadTopicKnowledge("/tmp", "topics", "", "")
	if got.Content != "" || got.Key != "" || got.Hash != "" {
		t.Errorf("empty topicKey must yield empty knowledge, got %+v", got)
	}
}

func TestLoadTopicKnowledge_MissingFile(t *testing.T) {
	dir := t.TempDir()
	got := LoadTopicKnowledge(dir, "topics", "nonexistent", "")
	if got.Content != "" {
		t.Errorf("missing file must yield empty content, got %q", got.Content)
	}
}

func TestLoadTopicKnowledge_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteTopic(t, dir, "topics", "blank", "   \n\t  ")
	got := LoadTopicKnowledge(dir, "topics", "blank", "")
	if got.Content != "" {
		t.Errorf("whitespace-only file must yield empty content, got %q", got.Content)
	}
}

func TestLoadTopicKnowledge_ReadsAndHashes(t *testing.T) {
	dir := t.TempDir()
	mustWriteTopic(t, dir, "topics", "coding", "Go 1.24, vLLM on DGX Spark.")
	got := LoadTopicKnowledge(dir, "topics", "coding", "")
	if got.Key != "coding" {
		t.Errorf("Key = %q, want coding", got.Key)
	}
	if !strings.Contains(got.Content, "vLLM") {
		t.Errorf("Content missing file body: %q", got.Content)
	}
	if len(got.Hash) != 12 {
		t.Errorf("Hash = %q, want 12 hex chars", got.Hash)
	}
}

func TestLoadTopicKnowledge_DefaultDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteTopic(t, dir, "topics", "work", "Q2 deals pipeline.")
	// Empty dir arg → defaults to "topics".
	got := LoadTopicKnowledge(dir, "", "work", "")
	if !strings.Contains(got.Content, "deals") {
		t.Errorf("default dir resolution failed: %q", got.Content)
	}
}

func TestLoadTopicKnowledge_Truncates(t *testing.T) {
	dir := t.TempDir()
	// Long body with Korean text — truncation must not split a multi-byte rune.
	big := strings.Repeat("한글지식 abcdefg ", 2000) // ~42 KB, well over the cap
	mustWriteTopic(t, dir, "topics", "big", big)
	got := LoadTopicKnowledge(dir, "topics", "big", "")
	if len(got.Content) == 0 {
		t.Fatalf("expected truncated content, got empty")
	}
	if len(got.Content) > maxTopicKnowledgeChars+64 {
		t.Errorf("content not truncated: %d bytes", len(got.Content))
	}
	if !utf8.ValidString(got.Content) {
		t.Errorf("truncation produced invalid UTF-8 (mid-rune cut)")
	}
}

func TestLoadTopicKnowledge_FrozenPerSession(t *testing.T) {
	ResetContextFileCacheForTest()
	dir := t.TempDir()
	mustWriteTopic(t, dir, "topics", "coding", "version one")
	sessionKey := "telegram:123:thread:42"

	first := LoadTopicKnowledge(dir, "topics", "coding", sessionKey)
	if !strings.Contains(first.Content, "version one") {
		t.Fatalf("first load wrong: %q", first.Content)
	}

	// Edit the file mid-session — the frozen snapshot must ignore the change.
	mustWriteTopic(t, dir, "topics", "coding", "version two")
	second := LoadTopicKnowledge(dir, "topics", "coding", sessionKey)
	if second.Content != first.Content {
		t.Errorf("frozen snapshot changed mid-session: %q -> %q", first.Content, second.Content)
	}

	// A different session (key) sees the edited content.
	other := LoadTopicKnowledge(dir, "topics", "coding", "telegram:123:thread:99")
	if !strings.Contains(other.Content, "version two") {
		t.Errorf("new session should see edited file: %q", other.Content)
	}
}

func TestLoadTopicKnowledge_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	// Write a secret one level above the knowledge dir.
	secret := filepath.Join(filepath.Dir(dir), "secret.md")
	_ = os.WriteFile(secret, []byte("TOP SECRET"), 0o600)
	defer os.Remove(secret)

	for _, bad := range []string{"../secret", "..", "sub/evil", `..\secret`} {
		got := LoadTopicKnowledge(dir, "topics", bad, "")
		if got.Content != "" {
			t.Errorf("traversal key %q must be rejected, got %q", bad, got.Content)
		}
	}
}

func mustWriteTopic(t *testing.T, workspace, dir, key, content string) {
	t.Helper()
	d := filepath.Join(workspace, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d, key+".md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write topic: %v", err)
	}
}
