package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/textchunk"
)

func TestSearchLateContextExpandsOnlyFinalWinnerWithAdjacentSections(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "wiki"), filepath.Join(t.TempDir(), "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	page := NewPage("배포 운영 기록", "시스템", nil)
	page.Body = strings.Join([]string{
		"## 배경", "이전 배포 배경 설명", "",
		"## 결정", "ORBIT-ALPHA 방식으로 전환한다", "",
		"## 검증", "스모크와 품질 검증을 수행한다", "",
		"## 무관", "점심 메뉴 기록",
	}, "\n")
	if err := store.WritePage("시스템/deploy.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	report, err := store.SearchWithOptions(context.Background(), "ORBIT-ALPHA", 1, QueryOptions{SkipRerank: true})
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(report.Results))
	}
	hit := report.Results[0]
	if !strings.Contains(hit.Content, "ORBIT-ALPHA") {
		t.Fatalf("precise match missing: %#v", hit)
	}
	for _, want := range []string{"## 배경", "## 결정", "## 검증"} {
		if !strings.Contains(hit.ExpandedContent, want) {
			t.Errorf("expanded context missing %q: %q", want, hit.ExpandedContent)
		}
	}
	if strings.Contains(hit.ExpandedContent, "## 무관") {
		t.Errorf("expanded context crossed the one-section neighbor bound: %q", hit.ExpandedContent)
	}
	if hit.ExpandedLine <= 0 || hit.ExpandedEndLine < hit.ExpandedLine {
		t.Errorf("expanded line range = %d-%d", hit.ExpandedLine, hit.ExpandedEndLine)
	}
	if report.Diagnostics.ContextExpanded != 1 {
		t.Errorf("ContextExpanded = %d, want 1", report.Diagnostics.ContextExpanded)
	}
}

func TestBoundedContextChunksKeepsMatchWhenNeighborsExceedBudget(t *testing.T) {
	chunks := []textchunk.Chunk{
		{Text: strings.Repeat("가", 80), StartLine: 1, EndLine: 2},
		{Text: "MATCH", StartLine: 3, EndLine: 3},
		{Text: strings.Repeat("나", 80), StartLine: 4, EndLine: 5},
	}
	got, start, end := boundedContextChunks(chunks, 1, 20)
	if got != "MATCH" || start != 3 || end != 3 {
		t.Fatalf("bounded = %q L%d-L%d, want MATCH L3-L3", got, start, end)
	}
}
