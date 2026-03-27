package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository root by walking up from the test file
// until it finds the docs/ directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	// The test runs from gateway-go/internal/chat; walk up to find docs/.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root with docs/ directory")
		}
		dir = parent
	}
}

func invokeManual(t *testing.T, workspaceDir string, params map[string]string) string {
	t.Helper()
	fn := toolSystemManual(workspaceDir)
	input, _ := json.Marshal(params)
	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("toolSystemManual returned error: %v", err)
	}
	return result
}

func invokeManualExpectErr(t *testing.T, workspaceDir string, params map[string]string) error {
	t.Helper()
	fn := toolSystemManual(workspaceDir)
	input, _ := json.Marshal(params)
	_, err := fn(context.Background(), input)
	return err
}

// --- topics ---

func TestSystemManualTopics(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{"action": "topics"})

	// Should contain the header.
	if !strings.Contains(result, "Deneb System Manual") {
		t.Error("expected 'Deneb System Manual' header in output")
	}
	// Should list categories that exist in docs/.
	for _, cat := range []string{"gateway/", "concepts/", "channels/"} {
		if !strings.Contains(result, cat) {
			t.Errorf("expected category %q in topics output", cat)
		}
	}
	// Should contain usage hint.
	if !strings.Contains(result, "polaris(action:'read'") {
		t.Error("expected read usage hint")
	}
}

func TestSystemManualTopicsWithFilter(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "topics",
		"topic":  "gateway",
	})

	// Header should mention the filter.
	if !strings.Contains(result, "gateway/") {
		t.Error("expected 'gateway/' in filtered output")
	}
	// Should NOT contain other categories.
	if strings.Contains(result, "channels/ (") {
		t.Error("filtered output should not contain channels/ category")
	}
}

// --- search ---

func TestSystemManualSearch(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "search",
		"query":  "session",
	})

	if !strings.Contains(result, "matches for") {
		t.Error("expected 'matches for' in search output")
	}
	// "session" is a common term; we expect at least one match.
	if strings.Contains(result, "No matches found") {
		t.Error("expected at least one match for 'session'")
	}
}

func TestSystemManualSearchNoResults(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "search",
		"query":  "xyzzy_nonexistent_term_42",
	})

	if !strings.Contains(result, "No matches found") {
		t.Errorf("expected 'No matches found', got: %s", result)
	}
}

// --- read ---

func TestSystemManualRead(t *testing.T) {
	root := repoRoot(t)
	// Read the root index.md which we know has frontmatter.
	result := invokeManual(t, root, map[string]string{
		"action": "read",
		"topic":  "index",
	})

	// Frontmatter should be stripped (no "---" block at the top).
	if strings.HasPrefix(result, "---") {
		t.Error("frontmatter was not stripped from read output")
	}
	// Should contain actual doc content.
	if !strings.Contains(result, "Deneb") {
		t.Error("expected doc content containing 'Deneb'")
	}
}

func TestSystemManualReadNotFound(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "read",
		"topic":  "nonexistent/page_that_does_not_exist",
	})

	if !strings.Contains(result, "Document not found") {
		t.Errorf("expected 'Document not found' message, got: %s", result)
	}
	// Should suggest using topics to browse.
	if !strings.Contains(result, "topics") {
		t.Error("expected suggestion to use topics action")
	}
}

// --- guides ---

func TestSystemManualGuidesList(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "guides",
	})

	if !strings.Contains(result, "Deneb System Guides") {
		t.Error("expected 'Deneb System Guides' header")
	}
	// Should list all built-in guides.
	for _, key := range builtinGuideOrder {
		if !strings.Contains(result, key) {
			t.Errorf("expected guide %q in listing", key)
		}
	}
}

func TestSystemManualGuidesRead(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "guides",
		"topic":  "aurora",
	})

	if !strings.Contains(result, "Aurora Context Engine") {
		t.Error("expected 'Aurora Context Engine' title in guide")
	}
	// Should contain actual guide content (starts with "# Aurora Context Engine").
	if !strings.HasPrefix(result, "# ") {
		t.Error("expected guide to start with markdown heading")
	}
}

func TestSystemManualGuidesUnknown(t *testing.T) {
	root := repoRoot(t)
	result := invokeManual(t, root, map[string]string{
		"action": "guides",
		"topic":  "nonexistent_guide",
	})

	if !strings.Contains(result, "Unknown guide") {
		t.Errorf("expected 'Unknown guide' message, got: %s", result)
	}
	// Should suggest listing guides.
	if !strings.Contains(result, "polaris(action:'guides')") {
		t.Error("expected suggestion to list available guides")
	}
}

// --- parseFrontmatter ---

func TestParseFrontmatter(t *testing.T) {
	content := "---\ntitle: \"My Page\"\nsummary: \"A short summary.\"\nread_when:\n  - Testing\n---\n\n# My Page\n\nBody content here."

	title, summary, body := parseFrontmatter(content)

	if title != "My Page" {
		t.Errorf("title = %q, want %q", title, "My Page")
	}
	if summary != "A short summary." {
		t.Errorf("summary = %q, want %q", summary, "A short summary.")
	}
	if strings.Contains(body, "---") {
		t.Error("body should not contain frontmatter delimiters")
	}
	if !strings.Contains(body, "Body content here.") {
		t.Error("body should contain the actual content")
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome plain markdown without frontmatter."

	title, summary, body := parseFrontmatter(content)

	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
	if summary != "" {
		t.Errorf("summary = %q, want empty", summary)
	}
	if body != content {
		t.Error("body should equal the original content when no frontmatter present")
	}
}
