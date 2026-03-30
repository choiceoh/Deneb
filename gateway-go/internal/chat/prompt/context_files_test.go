package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContextFiles(t *testing.T) {
	// Create a temp workspace with context files.
	dir := t.TempDir()

	claudeMd := "# Project Agent\nThis agent helps with coding."
	soulMd := "# Soul\nBe helpful and concise."

	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMd), 0o644)
	os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(soulMd), 0o644)

	files := LoadContextFiles(dir)

	if len(files) != 2 {
		t.Fatalf("expected 2 context files, got %d", len(files))
	}

	if files[0].Path != "CLAUDE.md" {
		t.Errorf("expected first file to be CLAUDE.md, got %q", files[0].Path)
	}
	if files[0].Content != claudeMd {
		t.Errorf("unexpected CLAUDE.md content: %q", files[0].Content)
	}
	if files[1].Path != "SOUL.md" {
		t.Errorf("expected second file to be SOUL.md, got %q", files[1].Path)
	}
}

func TestLoadContextFilesEmpty(t *testing.T) {
	files := LoadContextFiles("")
	if files != nil {
		t.Errorf("expected nil for empty workspace dir, got %v", files)
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

func TestFormatContextFilesForPrompt(t *testing.T) {
	files := []ContextFile{
		{Path: "CLAUDE.md", Content: "agent content"},
		{Path: "SOUL.md", Content: "soul content"},
	}

	result := FormatContextFilesForPrompt(files)
	if !strings.Contains(result, "# Project Context") {
		t.Error("expected Project Context heading")
	}
	if !strings.Contains(result, "## CLAUDE.md") {
		t.Error("expected CLAUDE.md section")
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

	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("fact-v1"), 0o644)

	// First load with session key — populates snapshot.
	files := LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v1" {
		t.Fatalf("expected MEMORY.md with fact-v1, got %v", files)
	}

	// Mutate file on disk (simulates mid-session memory export).
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("fact-v2"), 0o644)

	// Same session key — must return frozen snapshot (fact-v1).
	files = LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v1" {
		t.Fatalf("expected frozen snapshot fact-v1, got %q", files[0].Content)
	}

	// Different session key — gets fresh content.
	ResetContextFileCacheForTest() // clear mtime cache to force re-read
	files = LoadContextFiles(dir, WithSessionSnapshot("s2"))
	if len(files) != 1 || files[0].Content != "fact-v2" {
		t.Fatalf("expected fresh fact-v2 for new session, got %q", files[0].Content)
	}

	// Clear snapshot — next call for s1 loads fresh.
	ClearSessionSnapshot("s1")
	ResetContextFileCacheForTest()
	files = LoadContextFiles(dir, WithSessionSnapshot("s1"))
	if len(files) != 1 || files[0].Content != "fact-v2" {
		t.Fatalf("expected fresh fact-v2 after clear, got %q", files[0].Content)
	}
}

func TestSessionSnapshotWithSkipMemory(t *testing.T) {
	ResetContextFileCacheForTest()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("agent"), 0o644)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("facts"), 0o644)

	// Load with session key (no skip) — snapshot stores both files.
	files := LoadContextFiles(dir, WithSessionSnapshot("s3"))
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Load with same session key + skipMemory — returns snapshot minus MEMORY.md.
	files = LoadContextFiles(dir, WithSessionSnapshot("s3"), WithSkipMemory())
	if len(files) != 1 || files[0].Path != "CLAUDE.md" {
		t.Fatalf("expected only CLAUDE.md with skipMemory, got %v", files)
	}

	ClearSessionSnapshot("s3")
}

func TestFormatContextFilesForPrompt_NoSoul(t *testing.T) {
	files := []ContextFile{
		{Path: "CLAUDE.md", Content: "agent content"},
		{Path: "TOOLS.md", Content: "tools content"},
	}

	result := FormatContextFilesForPrompt(files)
	if strings.Contains(result, "embody its persona and tone") {
		t.Error("SOUL.md activation instruction should not appear without SOUL.md")
	}
	if !strings.Contains(result, "The following project context files have been loaded:") {
		t.Error("expected context files loaded preamble")
	}
}
