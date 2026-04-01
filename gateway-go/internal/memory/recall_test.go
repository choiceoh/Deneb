package memory

import (
	"context"
	"strings"
	"testing"
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

func TestFindBackfillCandidates(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "팩트 A — 엔티티 있음", Category: CategoryContext, Importance: 0.6})
	id2, _ := s.InsertFact(ctx, Fact{Content: "팩트 B — 엔티티 없음", Category: CategoryContext, Importance: 0.6})

	eid, _ := s.UpsertEntity(ctx, "TestEntity", EntityConcept)
	_ = s.LinkFactEntity(ctx, id1, eid, "subject")

	candidates := []SearchResult{
		{Fact: Fact{ID: id1}, Score: 0.9},
		{Fact: Fact{ID: id2}, Score: 0.8},
	}

	backfillIDs := findBackfillCandidates(ctx, s, candidates, 20)

	if len(backfillIDs) != 1 {
		t.Fatalf("expected 1 backfill candidate, got %d", len(backfillIDs))
	}
	if backfillIDs[0] != id2 {
		t.Errorf("expected backfill ID=%d, got %d", id2, backfillIDs[0])
	}
}

func TestBuildRecallPrompt(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 42, Content: "SQLite 선호", Category: CategoryPreference, Importance: 0.8}, Score: 0.9},
	}
	backfillIDs := []int64{42}

	prompt := buildRecallPrompt("SQLite 관련 결정", candidates, backfillIDs)

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "SQLite 관련 결정") {
		t.Error("prompt should contain user message")
	}
	if !strings.Contains(prompt, "id:42") {
		t.Error("prompt should contain fact ID")
	}
	if !strings.Contains(prompt, "빈 팩트 ID: [42]") {
		t.Error("prompt should contain backfill IDs")
	}
}

func TestFormatCandidatesAsKnowledge(t *testing.T) {
	candidates := []SearchResult{
		{Fact: Fact{ID: 1, Content: "테스트 팩트", Category: CategoryContext, Importance: 0.6}, Score: 0.8},
	}

	result := formatCandidatesAsKnowledge(candidates)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "### 메모리") {
		t.Error("result should contain header")
	}
	if !strings.Contains(result, "테스트 팩트") {
		t.Error("result should contain fact content")
	}
}

func TestFormatCandidatesAsKnowledge_Empty(t *testing.T) {
	result := formatCandidatesAsKnowledge(nil)
	if result != "" {
		t.Errorf("expected empty result for nil candidates, got %q", result)
	}
}

