package memory

import (
	"context"
	"testing"
)

func TestInsertRelation(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "SQLite를 선호함", Category: CategoryPreference, Importance: 0.8})
	id2, _ := s.InsertFact(ctx, Fact{Content: "PostgreSQL 검토 후 SQLite 유지 결정", Category: CategoryDecision, Importance: 0.9})

	err := s.InsertRelation(ctx, id1, id2, RelationEvolves, 0.9)
	if err != nil {
		t.Fatalf("InsertRelation: %v", err)
	}

	// Upsert: same relation should not fail.
	err = s.InsertRelation(ctx, id1, id2, RelationEvolves, 0.5)
	if err != nil {
		t.Fatalf("InsertRelation upsert: %v", err)
	}

	// Different relation type between same facts should succeed.
	err = s.InsertRelation(ctx, id1, id2, RelationSupports, 0.7)
	if err != nil {
		t.Fatalf("InsertRelation different type: %v", err)
	}
}

func TestGetRelatedFacts(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "초기 선호: SQLite", Category: CategoryPreference, Importance: 0.7})
	id2, _ := s.InsertFact(ctx, Fact{Content: "재확인: SQLite 유지", Category: CategoryPreference, Importance: 0.8})
	id3, _ := s.InsertFact(ctx, Fact{Content: "PostgreSQL 성능 이슈", Category: CategoryContext, Importance: 0.6})

	_ = s.InsertRelation(ctx, id1, id2, RelationEvolves, 1.0)
	_ = s.InsertRelation(ctx, id3, id2, RelationCauses, 0.8)

	related, err := s.GetRelatedFacts(ctx, id2)
	if err != nil {
		t.Fatalf("GetRelatedFacts: %v", err)
	}

	if len(related) != 2 {
		t.Fatalf("expected 2 related facts, got %d", len(related))
	}

	// Verify both directions are captured.
	hasOutgoing := false
	hasIncoming := false
	for _, rf := range related {
		if rf.Direction == "outgoing" {
			hasOutgoing = true
		}
		if rf.Direction == "incoming" {
			hasIncoming = true
		}
	}
	if hasOutgoing {
		t.Error("expected no outgoing relations from id2")
	}
	if !hasIncoming {
		t.Error("expected incoming relations to id2")
	}
}

func TestGetRelationChain(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "v1: SQLite 시작", Category: CategoryDecision, Importance: 0.6})
	id2, _ := s.InsertFact(ctx, Fact{Content: "v2: WAL 모드 도입", Category: CategoryDecision, Importance: 0.7})
	id3, _ := s.InsertFact(ctx, Fact{Content: "v3: 통합 DB로 전환", Category: CategoryDecision, Importance: 0.8})

	_ = s.InsertRelation(ctx, id1, id2, RelationEvolves, 1.0)
	_ = s.InsertRelation(ctx, id2, id3, RelationEvolves, 1.0)

	chain, err := s.GetRelationChain(ctx, id1, RelationEvolves, 5)
	if err != nil {
		t.Fatalf("GetRelationChain: %v", err)
	}

	if len(chain) != 2 {
		t.Fatalf("expected chain length 2, got %d", len(chain))
	}
	if chain[0].ID != id2 {
		t.Errorf("expected chain[0].ID=%d, got %d", id2, chain[0].ID)
	}
	if chain[1].ID != id3 {
		t.Errorf("expected chain[1].ID=%d, got %d", id3, chain[1].ID)
	}
}

func TestSupersedeFact_CreatesRelation(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	oldID, _ := s.InsertFact(ctx, Fact{Content: "구 팩트", Category: CategoryContext, Importance: 0.5})
	newID, _ := s.InsertFact(ctx, Fact{Content: "신 팩트", Category: CategoryContext, Importance: 0.7})

	err := s.SupersedeFact(ctx, oldID, newID)
	if err != nil {
		t.Fatalf("SupersedeFact: %v", err)
	}

	// Verify the evolves relation was created.
	related, err := s.GetRelatedFacts(ctx, newID)
	if err != nil {
		t.Fatalf("GetRelatedFacts: %v", err)
	}

	found := false
	for _, rf := range related {
		if rf.RelationType == RelationEvolves && rf.Direction == "incoming" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected evolves relation from SupersedeFact")
	}
}
