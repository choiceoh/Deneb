package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExpandViaEntities(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	// Create facts and entities.
	id1, _ := s.InsertFact(ctx, Fact{Content: "SGLang 포트 30000 설정", Category: CategoryDecision, Importance: 0.8})
	id2, _ := s.InsertFact(ctx, Fact{Content: "SGLang 모델 경로 변경", Category: CategoryContext, Importance: 0.6})
	id3, _ := s.InsertFact(ctx, Fact{Content: "Docker 설정 완료", Category: CategoryContext, Importance: 0.5})

	eid, _ := s.UpsertEntity(ctx, "SGLang", EntityTool)
	_ = s.LinkFactEntity(ctx, id1, eid, "subject")
	_ = s.LinkFactEntity(ctx, id2, eid, "subject")

	// Start with only id1 as a candidate.
	candidates := []SearchResult{
		{Fact: Fact{ID: id1, Content: "SGLang 포트 30000 설정", Category: CategoryDecision, Importance: 0.8, Active: true}, Score: 0.9},
	}

	expanded := expandViaEntities(ctx, s, candidates, 20)

	// Should have found id2 via entity expansion, but not id3 (no entity link).
	if len(expanded) != 1 {
		t.Fatalf("expected 1 expanded fact, got %d", len(expanded))
	}
	if expanded[0].Fact.ID != id2 {
		t.Errorf("expected expanded fact ID=%d, got %d", id2, expanded[0].Fact.ID)
	}
	_ = id3 // id3 should not appear
}

func TestExpandViaRelations(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "v1: SQLite 도입", Category: CategoryDecision, Importance: 0.7})
	id2, _ := s.InsertFact(ctx, Fact{Content: "v2: WAL 모드 추가", Category: CategoryDecision, Importance: 0.8})
	_ = s.InsertRelation(ctx, id1, id2, RelationEvolves, 1.0)

	candidates := []SearchResult{
		{Fact: Fact{ID: id1, Content: "v1: SQLite 도입", Category: CategoryDecision, Importance: 0.7, Active: true}, Score: 0.9},
	}

	expanded := expandViaRelations(ctx, s, candidates, 3)

	if len(expanded) != 1 {
		t.Fatalf("expected 1 expanded fact via relation, got %d", len(expanded))
	}
	if expanded[0].Fact.ID != id2 {
		t.Errorf("expected expanded fact ID=%d, got %d", id2, expanded[0].Fact.ID)
	}
}

func TestMergeSearchResults(t *testing.T) {
	a := []SearchResult{
		{Fact: Fact{ID: 1}, Score: 0.9},
		{Fact: Fact{ID: 2}, Score: 0.8},
	}
	b := []SearchResult{
		{Fact: Fact{ID: 2}, Score: 0.5}, // duplicate
		{Fact: Fact{ID: 3}, Score: 0.7},
	}

	merged := mergeSearchResults(a, b)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
}

func TestRerankCandidates(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Content: "저관련 팩트"}, Score: 0.3},
		{Fact: Fact{ID: 2, Content: "고관련 팩트"}, Score: 0.5},
		{Fact: Fact{ID: 3, Content: "중관련 팩트"}, Score: 0.4},
	}

	// Mock reranker: returns reversed order (ID=3 highest, ID=1 lowest).
	mockReranker := func(ctx context.Context, query string, docs []string, topN int) ([]RerankResult, error) {
		return []RerankResult{
			{Index: 1, RelevanceScore: 0.95}, // ID=2
			{Index: 2, RelevanceScore: 0.80}, // ID=3
			{Index: 0, RelevanceScore: 0.30}, // ID=1
		}, nil
	}

	ctx := context.Background()
	result := rerankCandidates(ctx, mockReranker, "테스트 쿼리", candidates, nil)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	// First result should be ID=2 (highest reranker score).
	if result[0].Fact.ID != 2 {
		t.Errorf("expected first result ID=2, got %d", result[0].Fact.ID)
	}
}

func TestBuildTimeline(t *testing.T) {
	now := time.Now()
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Content: "두번째 일", CreatedAt: now}},
		{Fact: Fact{ID: 2, Content: "첫번째 일", CreatedAt: now.Add(-24 * time.Hour)}},
	}

	timeline := buildTimeline(candidates)
	if timeline == "" {
		t.Fatal("expected non-empty timeline")
	}
	// Timeline should have arrow separator.
	if !strings.Contains(timeline, "→") {
		t.Error("timeline should contain arrow separator")
	}
}

func TestBuildTimeline_SingleFact(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Content: "유일한 팩트", CreatedAt: time.Now()}},
	}
	timeline := buildTimeline(candidates)
	if timeline != "" {
		t.Errorf("expected empty timeline for single fact, got %q", timeline)
	}
}

func TestBuildEntitySummary(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Category: CategoryDecision}, Score: 0.9},
		{Fact: Fact{ID: 2, Category: CategoryContext}, Score: 0.8},
	}
	entityNames := map[int64][]string{
		1: {"SGLang"},
		2: {"SGLang"},
	}

	summary := buildEntitySummary(candidates, entityNames)
	if summary == "" {
		t.Fatal("expected non-empty entity summary")
	}
	if !strings.Contains(summary, "SGLang") {
		t.Error("entity summary should contain entity name")
	}
	if !strings.Contains(summary, "2개 팩트") {
		t.Error("entity summary should contain fact count")
	}
}

func TestBuildEntitySummary_NoEntities(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 999}, Score: 0.9},
	}
	entityNames := map[int64][]string{}

	summary := buildEntitySummary(candidates, entityNames)
	if summary != "" {
		t.Errorf("expected empty summary for facts with no entities, got %q", summary)
	}
}

func TestFormatRecallKnowledge(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Content: "테스트 팩트", Category: CategoryContext, Importance: 0.6, CreatedAt: time.Now()}, Score: 0.8},
	}
	entityNames := map[int64][]string{}

	result := formatRecallKnowledge(candidates, entityNames)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "### 메모리 (recall)") {
		t.Error("result should contain recall header")
	}
	if !strings.Contains(result, "테스트 팩트") {
		t.Error("result should contain fact content")
	}
}

func TestFormatRecallKnowledge_Empty(t *testing.T) {
	result := formatRecallKnowledge(nil, nil)
	if result != "" {
		t.Errorf("expected empty result for nil candidates, got %q", result)
	}
}

func TestTruncateContent(t *testing.T) {
	short := "짧은 텍스트"
	if truncateContent(short, 40) != short {
		t.Error("short text should not be truncated")
	}

	long := strings.Repeat("가", 50)
	truncated := truncateContent(long, 10)
	if !strings.HasSuffix(truncated, "...") {
		t.Error("truncated text should end with ...")
	}
	// 10 runes + "..."
	runes := []rune(truncated)
	if len(runes) != 13 { // 10 + 3 for "..."
		t.Errorf("expected 13 runes, got %d", len(runes))
	}
}
