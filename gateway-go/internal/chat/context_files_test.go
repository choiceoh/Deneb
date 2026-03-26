package chat

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
}
