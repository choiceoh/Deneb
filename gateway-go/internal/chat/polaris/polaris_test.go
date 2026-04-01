package polaris

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

func docsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "docs")
}

// --- handler (question-only interface) ---

func TestPolarisAskBasic(t *testing.T) {
	root := repoRoot(t)
	called := false
	mockLLM := func(_ context.Context, system, user string, maxTokens int) (string, error) {
		called = true
		if !strings.Contains(user, "## Question") {
			t.Error("expected '## Question' section in LLM input")
		}
		return "세션은 IDLE에서 RUNNING으로 전환됩니다.", nil
	}
	mockHealth := func() bool { return true }

	fn := NewHandlerWithDeps(root, Deps{LLM: mockLLM, Health: mockHealth})
	input, _ := json.Marshal(map[string]string{
		"question": "세션 라이프사이클은 어떻게 동작하나요?",
	})
	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("ask returned error: %v", err)
	}
	if !called {
		t.Error("expected LLM synthesizer to be called")
	}
	if !strings.Contains(result, "세션") {
		t.Errorf("expected synthesized answer about sessions, got: %s", result)
	}
}

func TestPolarisAskWithCodeSearch(t *testing.T) {
	root := repoRoot(t)
	var toolsCalled []string
	mockTools := &recordingToolExecutor{calls: &toolsCalled}
	mockLLM := func(_ context.Context, system, user string, maxTokens int) (string, error) {
		// Verify code sources are included when ToolExecutor is available.
		if !strings.Contains(user, "## Relevant") {
			t.Error("expected relevant context sections in LLM input")
		}
		return "Answer with code context.", nil
	}
	mockHealth := func() bool { return true }

	fn := NewHandlerWithDeps(root, Deps{
		LLM:    mockLLM,
		Health: mockHealth,
		Tools:  &ReadOnlyExecutor{Inner: mockTools},
	})
	input, _ := json.Marshal(map[string]string{
		"question": "session lifecycle state machine",
	})
	_, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("ask with tools returned error: %v", err)
	}
	// Should have called grep via ToolExecutor.
	hasGrep := false
	for _, name := range toolsCalled {
		if name == "grep" {
			hasGrep = true
		}
	}
	if !hasGrep {
		t.Error("expected grep tool to be called for code search")
	}
}

func TestPolarisAskFallback(t *testing.T) {
	root := repoRoot(t)
	mockLLM := func(_ context.Context, system, user string, maxTokens int) (string, error) {
		t.Error("LLM should not be called when health check fails")
		return "", nil
	}
	mockHealth := func() bool { return false }

	fn := NewHandlerWithDeps(root, Deps{LLM: mockLLM, Health: mockHealth})
	input, _ := json.Marshal(map[string]string{
		"question": "session lifecycle",
	})
	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("ask fallback returned error: %v", err)
	}
	if !strings.Contains(result, "sglang") {
		t.Error("expected fallback message mentioning sglang")
	}
	if !strings.Contains(result, "session lifecycle") {
		t.Error("expected question echoed in fallback")
	}
}

func TestPolarisAskEmptyQuestion(t *testing.T) {
	root := repoRoot(t)
	fn := NewHandlerWithDeps(root, Deps{})
	input, _ := json.Marshal(map[string]string{
		"question": "",
	})
	_, err := fn(context.Background(), input)
	if err == nil {
		t.Error("expected error for empty question")
	}
}

func TestPolarisAskNoLLM(t *testing.T) {
	root := repoRoot(t)
	fn := NewHandler(root)
	input, _ := json.Marshal(map[string]string{
		"question": "aurora context engine",
	})
	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("ask without LLM returned error: %v", err)
	}
	if !strings.Contains(result, "sglang") {
		t.Error("expected fallback message when no LLM injected")
	}
}

// --- ReadOnlyExecutor ---

func TestReadOnlyExecutorBlocks(t *testing.T) {
	mockInner := &mockToolExecutor{result: "ok"}
	ro := &ReadOnlyExecutor{Inner: mockInner}

	// Allowed tools should work.
	for _, name := range []string{"read", "grep", "find", "tree", "analyze", "diff"} {
		_, err := ro.Execute(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			t.Errorf("%s should be allowed: %v", name, err)
		}
	}

	// Write/mutation tools should be blocked.
	for _, name := range []string{"write", "edit", "exec", "process", "git", "multi_edit"} {
		_, err := ro.Execute(context.Background(), name, json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("%s should be blocked by ReadOnlyExecutor", name)
		}
	}
}

// --- internal function tests ---

func TestPolarisSearchInternal(t *testing.T) {
	dir := docsDir(t)
	results := polarisSearchInternal(dir, "session")
	if len(results) == 0 {
		t.Error("expected at least one result for 'session'")
	}
	// Results should be sorted by hit count descending.
	for i := 1; i < len(results); i++ {
		if results[i].HitCount > results[i-1].HitCount {
			t.Errorf("results not sorted by relevance: [%d].HitCount=%d > [%d].HitCount=%d",
				i, results[i].HitCount, i-1, results[i-1].HitCount)
		}
	}
}

func TestPolarisSearchInternalNoResults(t *testing.T) {
	dir := docsDir(t)
	results := polarisSearchInternal(dir, "xyzzy_nonexistent_term_42")
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestPolarisTopics(t *testing.T) {
	dir := docsDir(t)
	result, err := polarisTopics(dir, "")
	if err != nil {
		t.Fatalf("polarisTopics error: %v", err)
	}
	if !strings.Contains(result, "Deneb System Manual") {
		t.Error("expected 'Deneb System Manual' header")
	}
	if !strings.Contains(result, "categories") {
		t.Error("expected 'categories' count")
	}
}

func TestPolarisRead(t *testing.T) {
	dir := docsDir(t)
	result, err := polarisRead(dir, "reference/RELEASING")
	if err != nil {
		t.Fatalf("polarisRead error: %v", err)
	}
	if strings.HasPrefix(result, "---") {
		t.Error("frontmatter was not stripped")
	}
	if !strings.Contains(result, "Deneb") {
		t.Error("expected doc content")
	}
}

func TestPolarisGuidesList(t *testing.T) {
	result, err := polarisGuides("")
	if err != nil {
		t.Fatalf("polarisGuides error: %v", err)
	}
	if !strings.Contains(result, "Deneb System Guides") {
		t.Error("expected 'Deneb System Guides' header")
	}
	for _, key := range builtinGuideOrder {
		if !strings.Contains(result, key) {
			t.Errorf("expected guide %q in listing", key)
		}
	}
}

func TestPolarisGuidesRead(t *testing.T) {
	result, err := polarisGuides("aurora")
	if err != nil {
		t.Fatalf("polarisGuides error: %v", err)
	}
	if !strings.Contains(result, "Aurora Context Engine") {
		t.Error("expected 'Aurora Context Engine' title")
	}
	if !strings.Contains(result, "## Related Guides") {
		t.Error("expected 'Related Guides' footer")
	}
}

// --- guide scoring ---

func TestScoreGuideRelevance(t *testing.T) {
	g := guideEntry{
		Key:     "aurora",
		Title:   "Aurora Context Engine",
		Summary: "Context assembly lifecycle, token budgeting, aurora tools",
		Content: "Aurora handles context assembly for sessions.",
	}

	score := scoreGuideRelevance(g, []string{"aurora"})
	if score == 0 {
		t.Error("expected non-zero score for 'aurora' keyword")
	}

	// Title matches should boost score higher than content-only.
	scoreTitle := scoreGuideRelevance(g, []string{"aurora"})
	scoreContent := scoreGuideRelevance(g, []string{"sessions"})
	if scoreTitle <= scoreContent {
		t.Errorf("title match (aurora=%d) should score higher than content-only (sessions=%d)",
			scoreTitle, scoreContent)
	}
}

// --- keyword extraction ---

func TestExtractSearchKeywords(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"세션 라이프사이클은 어떻게 동작하나요?", "세션"},
		{"aurora context engine", "aurora"},
		{"How does compaction work?", "compaction"},
	}
	for _, tc := range tests {
		result := extractSearchKeywords(tc.input)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("extractSearchKeywords(%q) = %q, expected to contain %q",
				tc.input, result, tc.contains)
		}
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
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome plain markdown."
	title, summary, body := parseFrontmatter(content)
	if title != "" || summary != "" {
		t.Error("expected empty title/summary for non-frontmatter content")
	}
	if body != content {
		t.Error("body should equal original content")
	}
}

// --- consistency checks ---

func TestGuideOrderMatchesMap(t *testing.T) {
	for _, key := range builtinGuideOrder {
		g, ok := builtinGuides[key]
		if !ok {
			t.Errorf("builtinGuideOrder contains %q but builtinGuides has no entry", key)
			continue
		}
		if g.Content == "" {
			t.Errorf("guide %q has empty content", key)
		}
		if g.Key != key {
			t.Errorf("guide %q: Key field is %q, want %q", key, g.Key, key)
		}
	}
	orderSet := make(map[string]bool, len(builtinGuideOrder))
	for _, key := range builtinGuideOrder {
		orderSet[key] = true
	}
	for key := range builtinGuides {
		if !orderSet[key] {
			t.Errorf("builtinGuides has %q but builtinGuideOrder does not", key)
		}
	}
}

func TestGuideCategoriesComplete(t *testing.T) {
	seen := make(map[string]string)
	for _, cat := range guideCategories {
		for _, key := range cat.Guides {
			if prev, ok := seen[key]; ok {
				t.Errorf("guide %q appears in both %q and %q", key, prev, cat.Key)
			}
			seen[key] = cat.Key
		}
	}
	for key := range builtinGuides {
		if _, ok := seen[key]; !ok {
			t.Errorf("guide %q is not in any category", key)
		}
	}
}

// --- test doubles ---

type mockToolExecutor struct {
	result string
}

func (m *mockToolExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return m.result, nil
}

type recordingToolExecutor struct {
	calls *[]string
}

func (r *recordingToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (string, error) {
	*r.calls = append(*r.calls, name)
	return "", nil
}
