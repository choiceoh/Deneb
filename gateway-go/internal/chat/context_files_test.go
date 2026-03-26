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

	agentsMd := "# Project Agent\nThis agent helps with coding."
	soulMd := "# Soul\nBe helpful and concise."

	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsMd), 0o644)
	os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(soulMd), 0o644)

	files := LoadContextFiles(dir)

	if len(files) != 2 {
		t.Fatalf("expected 2 context files, got %d", len(files))
	}

	if files[0].Path != "AGENTS.md" {
		t.Errorf("expected first file to be AGENTS.md, got %q", files[0].Path)
	}
	if files[0].Content != agentsMd {
		t.Errorf("unexpected AGENTS.md content: %q", files[0].Content)
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

func TestLoadContextFilesSymlinkDedup(t *testing.T) {
	dir := t.TempDir()
	content := "# Same Content"
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o644)
	// Create CLAUDE.md as symlink to AGENTS.md.
	os.Symlink(filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md"))

	files := LoadContextFiles(dir)

	// Should only load one (symlink dedup).
	if len(files) != 1 {
		t.Fatalf("expected 1 file (symlink dedup), got %d", len(files))
	}
}

func TestTruncateContent(t *testing.T) {
	content := strings.Repeat("a", 100)
	result := truncateContent(content, 50)
	if len(result) > 60 { // 50 + marker overhead
		t.Errorf("truncated content too long: %d chars", len(result))
	}
	if !strings.Contains(result, "[...truncated...]") {
		t.Error("expected truncation marker")
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
}
