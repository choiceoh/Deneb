package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func TestPrefetch_EmptyMessage(t *testing.T) {
	result := Prefetch(context.Background(), "", Deps{})
	if result != "" {
		t.Errorf("expected empty for empty message, got: %q", result)
	}
}

func TestPrefetch_NoDeps(t *testing.T) {
	result := Prefetch(context.Background(), "비금도 해상태양광 진행상황 알려줘", Deps{})
	if result != "" {
		t.Errorf("expected empty with no deps, got: %q", result)
	}
}

func TestPrefetch_MemoryOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# 프로젝트 메모\n비금도 3차 계통연계 승인 완료\n"), 0o644)

	deps := Deps{WorkspaceDir: dir}
	result := Prefetch(context.Background(), "비금도 계통연계 현황 알려줘", deps)

	if !strings.Contains(result, "메모리") {
		t.Errorf("expected memory section, got: %q", result)
	}
	if !strings.Contains(result, "비금도") {
		t.Errorf("expected memory match, got: %q", result)
	}
}

func TestPrefetch_NoResults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("unrelated content\n"), 0o644)

	deps := Deps{WorkspaceDir: dir}
	result := Prefetch(context.Background(), "xyznonexistent", deps)

	if result != "" {
		t.Errorf("expected empty for no results, got: %q", result)
	}
}

func TestFormatKnowledge_TokenBudget(t *testing.T) {
	var matches []chattools.MemoryMatch
	longContent := strings.Repeat("가나다라마바사 ", 200)
	for i := 0; i < 20; i++ {
		matches = append(matches, chattools.MemoryMatch{
			File:    "MEMORY.md",
			Line:    i + 1,
			Snippet: longContent,
		})
	}

	formatted := formatKnowledge(matches)
	tokens := len(formatted) / charsPerToken
	if tokens > knowledgeMaxTokens+500 {
		t.Errorf("exceeded token budget: %d tokens (max %d)", tokens, knowledgeMaxTokens)
	}
}

func TestSearchMemoryFiles_Shared(t *testing.T) {
	dir := t.TempDir()
	content := "# Notes\nGolang is great\nRust is fast\nPython is easy\n"
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644)

	t.Run("finds matches", func(t *testing.T) {
		matches := chattools.SearchMemoryFiles(dir, "golang", 10)
		if len(matches) == 0 {
			t.Fatal("expected matches")
		}
		if matches[0].File != "MEMORY.md" {
			t.Errorf("expected MEMORY.md, got %q", matches[0].File)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		matches := chattools.SearchMemoryFiles(dir, "is", 1)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match (limit), got %d", len(matches))
		}
	})

	t.Run("empty query", func(t *testing.T) {
		matches := chattools.SearchMemoryFiles(dir, "", 10)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches for empty query, got %d", len(matches))
		}
	})
}
