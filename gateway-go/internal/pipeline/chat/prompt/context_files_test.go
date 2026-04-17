package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadContextFiles(t *testing.T) {
	ResetContextFileCacheForTest()
	dir := t.TempDir()

	agentsMd := "# Project Agent\nThis agent helps with coding."
	soulMd := "# Soul\nBe helpful and concise."

	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsMd), 0o644)
	os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(soulMd), 0o644)

	files := LoadContextFiles(dir)

	if len(files) != 2 {
		t.Fatalf("got %d, want 2 context files", len(files))
	}

	if files[0].Path != "AGENTS.md" {
		t.Errorf("got %q, want first file to be AGENTS.md", files[0].Path)
	}
	if files[0].Content != agentsMd {
		t.Errorf("unexpected AGENTS.md content: %q", files[0].Content)
	}
	if files[1].Path != "SOUL.md" {
		t.Errorf("got %q, want second file to be SOUL.md", files[1].Path)
	}
}

func TestTruncateContent(t *testing.T) {
	content := strings.Repeat("a", 100)
	result := truncateContent(content, 50)
	if len(result) > 70 { // 50 + marker overhead (~20 chars for "[...truncated...]" + newlines)
		t.Errorf("truncated content too long: %d chars", len(result))
	}
	if !strings.Contains(result, "[...truncated...]") {
		t.Error("expected truncation marker")
	}
}

// TestTruncateContent_UTF8Safe verifies that truncating Korean / multi-byte
// content never splits a rune at the head or tail cut. An earlier bug used
// byte indexing, which produced invalid UTF-8 in the system prompt for any
// context file bigger than the per-file cap — a near-certain case for a
// Korean-first project.
func TestTruncateContent_UTF8Safe(t *testing.T) {
	// "한" = 3 bytes (UTF-8). A 3000-rune string is ~9KB, well over a 4KB cap.
	content := strings.Repeat("한", 3000)
	result := truncateContent(content, 4000)

	if !utf8.ValidString(result) {
		t.Fatalf("truncated content is not valid UTF-8")
	}
	if !strings.Contains(result, "[...truncated...]") {
		t.Error("expected truncation marker")
	}
	// Neither head nor tail should exceed their nominal byte budget (defensive:
	// the whole point is we never grow past the cap to preserve a rune).
	if len(result) > 4000+len("\n\n[...truncated...]\n\n") {
		t.Errorf("truncated content too long: %d bytes", len(result))
	}
}

func TestClipHeadUTF8(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty cap", "한글", 0, ""},
		{"cap exceeds length", "abc", 10, "abc"},
		{"ascii exact cut", "abcdef", 3, "abc"},
		{"inside multibyte rune clips short", "한글", 2, ""},  // 2 lands inside first 3-byte rune
		{"exactly one multibyte rune", "한글", 3, "한"},        // 3 = rune boundary
		{"cut at rune boundary mid-string", "a한b", 4, "a한"}, // "a" + 3 bytes of "한" = 4
		{"cut inside second rune", "a한b", 3, "a"},           // would split "한"
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clipHeadUTF8(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("clipHeadUTF8(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8")
			}
		})
	}
}

func TestClipTailUTF8(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty cap", "한글", 0, ""},
		{"cap exceeds length", "abc", 10, "abc"},
		{"ascii exact cut", "abcdef", 3, "def"},
		{"cut inside multibyte rune clips short", "한글", 2, ""}, // tail start lands inside "글"
		{"cut at rune boundary", "한글", 3, "글"},
		{"cut inside leading rune", "한b", 1, "b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clipTailUTF8(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("clipTailUTF8(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8")
			}
		})
	}
}

func TestFormatContextFilesForPrompt(t *testing.T) {
	files := []ContextFile{
		{Path: "AGENTS.md", Content: "agent content"},
		{Path: "SOUL.md", Content: "soul content"},
	}

	result := FormatContextFilesForPrompt(files)
	if !strings.Contains(result, "# Project Context") {
		t.Error("expected Project Context heading")
	}
	if !strings.Contains(result, "## AGENTS.md") {
		t.Error("expected AGENTS.md section")
	}
	if !strings.Contains(result, "agent content") {
		t.Error("expected agent content")
	}
	if !strings.Contains(result, "The following project context files have been loaded:") {
		t.Error("expected context files loaded preamble")
	}
	if !strings.Contains(result, "embody its persona and tone") {
		t.Error("expected SOUL.md activation instruction when SOUL.md is present")
	}
}

func TestSessionSnapshotFrozen(t *testing.T) {
	ResetContextFileCacheForTest()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("fact-v1"), 0o644)

	// First load with session key — populates snapshot.
	files := LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v1" {
		t.Fatalf("got %v, want AGENTS.md with fact-v1", files)
	}

	// Mutate file on disk (simulates mid-session update).
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("fact-v2"), 0o644)

	// Same session key — must return frozen snapshot (fact-v1).
	files = LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v1" {
		t.Fatalf("got %q, want frozen snapshot fact-v1", files[0].Content)
	}

	// Different session key — gets fresh content.
	ResetContextFileCacheForTest() // clear mtime cache to force re-read
	files = LoadContextFiles(dir, WithSessionSnapshot("s2"))
	if len(files) != 1 || files[0].Content != "fact-v2" {
		t.Fatalf("got %q, want fresh fact-v2 for new session", files[0].Content)
	}

	// Clear snapshot — next call for s1 loads fresh.
	ClearSessionSnapshot("s1")
	ResetContextFileCacheForTest()
	files = LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v2" {
		t.Fatalf("got %q, want fresh fact-v2 after clear", files[0].Content)
	}
}
