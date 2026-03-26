package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// mockVegaBackend implements vega.Backend for testing.
type mockVegaBackend struct {
	results []vega.SearchResult
	err     error
}

func (m *mockVegaBackend) Execute(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (m *mockVegaBackend) Search(_ context.Context, _ string, _ vega.SearchOpts) ([]vega.SearchResult, error) {
	return m.results, m.err
}
func (m *mockVegaBackend) Close() error { return nil }

func TestPrefetchKnowledge_EmptyMessage(t *testing.T) {
	result := PrefetchKnowledge(context.Background(), "", KnowledgeDeps{})
	if result != "" {
		t.Errorf("expected empty for empty message, got: %q", result)
	}
}

func TestPrefetchKnowledge_NoDeps(t *testing.T) {
	result := PrefetchKnowledge(context.Background(), "비금도 진행상황", KnowledgeDeps{})
	if result != "" {
		t.Errorf("expected empty with no deps, got: %q", result)
	}
}

func TestPrefetchKnowledge_VegaOnly(t *testing.T) {
	backend := &mockVegaBackend{
		results: []vega.SearchResult{
			{ProjectName: "비금도 해상태양광", Section: "현재 상황", Content: "해저케이블 154kV 설치 진행중", Score: 0.85},
		},
	}
	deps := KnowledgeDeps{VegaBackend: backend}
	result := PrefetchKnowledge(context.Background(), "비금도", deps)

	if !strings.Contains(result, "관련 지식") {
		t.Errorf("expected '관련 지식' header, got: %q", result)
	}
	if !strings.Contains(result, "비금도 해상태양광") {
		t.Errorf("expected project name, got: %q", result)
	}
	if !strings.Contains(result, "해저케이블") {
		t.Errorf("expected content, got: %q", result)
	}
}

func TestPrefetchKnowledge_MemoryOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# 프로젝트 메모\n비금도 3차 계통연계 승인 완료\n"), 0o644)

	deps := KnowledgeDeps{WorkspaceDir: dir}
	result := PrefetchKnowledge(context.Background(), "비금도", deps)

	if !strings.Contains(result, "메모리") {
		t.Errorf("expected memory section, got: %q", result)
	}
	if !strings.Contains(result, "비금도") {
		t.Errorf("expected memory match, got: %q", result)
	}
}

func TestPrefetchKnowledge_BothSources(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("김대희 담당자 연락처 확인\n"), 0o644)

	backend := &mockVegaBackend{
		results: []vega.SearchResult{
			{ProjectName: "비금도", Section: "담당자", Content: "김대희 부장", Score: 0.9},
		},
	}
	deps := KnowledgeDeps{VegaBackend: backend, WorkspaceDir: dir}
	result := PrefetchKnowledge(context.Background(), "김대희", deps)

	if !strings.Contains(result, "프로젝트: 비금도") {
		t.Errorf("expected vega result, got: %q", result)
	}
	if !strings.Contains(result, "메모리") {
		t.Errorf("expected memory section, got: %q", result)
	}
}

func TestPrefetchKnowledge_NoResults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("unrelated content\n"), 0o644)

	backend := &mockVegaBackend{results: nil}
	deps := KnowledgeDeps{VegaBackend: backend, WorkspaceDir: dir}
	result := PrefetchKnowledge(context.Background(), "xyznonexistent", deps)

	if result != "" {
		t.Errorf("expected empty for no results, got: %q", result)
	}
}

func TestFormatKnowledge_TokenBudget(t *testing.T) {
	// Create results that would exceed the token budget.
	var results []vega.SearchResult
	longContent := strings.Repeat("가나다라마바사 ", 200) // ~1400 chars per result
	for i := 0; i < 20; i++ {
		results = append(results, vega.SearchResult{
			ProjectName: "프로젝트",
			Section:     "섹션",
			Content:     longContent,
			Score:       0.5,
		})
	}

	formatted := formatKnowledge(results, nil)
	tokens := estimateTokens(formatted)
	if tokens > knowledgeMaxTokens+500 { // allow small overshoot from last item
		t.Errorf("exceeded token budget: %d tokens (max %d)", tokens, knowledgeMaxTokens)
	}
}

func TestSearchMemoryFiles_Shared(t *testing.T) {
	dir := t.TempDir()
	content := "# Notes\nGolang is great\nRust is fast\nPython is easy\n"
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644)

	t.Run("finds matches", func(t *testing.T) {
		matches := searchMemoryFiles(dir, "golang", 10)
		if len(matches) == 0 {
			t.Fatal("expected matches")
		}
		if matches[0].File != "MEMORY.md" {
			t.Errorf("expected MEMORY.md, got %q", matches[0].File)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		matches := searchMemoryFiles(dir, "is", 1)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match (limit), got %d", len(matches))
		}
	})

	t.Run("empty query", func(t *testing.T) {
		matches := searchMemoryFiles(dir, "", 10)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches for empty query, got %d", len(matches))
		}
	})
}
