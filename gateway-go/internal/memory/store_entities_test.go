package memory

import (
	"context"
	"testing"
)

func TestUpsertEntity(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, err := s.UpsertEntity(ctx, "SGLang", EntityTool)
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive ID, got %d", id1)
	}

	// Upsert again: should return same ID with incremented mention_count.
	id2, err := s.UpsertEntity(ctx, "SGLang", EntityTool)
	if err != nil {
		t.Fatalf("UpsertEntity upsert: %v", err)
	}
	if id2 != id1 {
		t.Errorf("expected same ID %d on upsert, got %d", id1, id2)
	}

	e, err := s.GetEntity(ctx, "SGLang")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if e.MentionCount != 2 {
		t.Errorf("expected mention_count=2, got %d", e.MentionCount)
	}
	if e.EntityType != EntityTool {
		t.Errorf("expected entity_type=%s, got %s", EntityTool, e.EntityType)
	}
}

func TestUpsertEntity_UpgradesFromUnknown(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_, _ = s.UpsertEntity(ctx, "Fred", EntityUnknown)
	_, _ = s.UpsertEntity(ctx, "Fred", EntityPerson)

	e, _ := s.GetEntity(ctx, "Fred")
	if e.EntityType != EntityPerson {
		t.Errorf("expected entity_type upgrade to %s, got %s", EntityPerson, e.EntityType)
	}
}

func TestLinkFactEntity(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	factID, _ := s.InsertFact(ctx, Fact{Content: "SGLang 설정 변경", Category: CategoryContext, Importance: 0.6})
	entityID, _ := s.UpsertEntity(ctx, "SGLang", EntityTool)

	err := s.LinkFactEntity(ctx, factID, entityID, "subject")
	if err != nil {
		t.Fatalf("LinkFactEntity: %v", err)
	}

	// Duplicate link should be no-op.
	err = s.LinkFactEntity(ctx, factID, entityID, "subject")
	if err != nil {
		t.Fatalf("LinkFactEntity duplicate: %v", err)
	}
}

func TestGetFactsByEntity(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "SGLang 포트 30000", Category: CategoryDecision, Importance: 0.8})
	id2, _ := s.InsertFact(ctx, Fact{Content: "SGLang 모델 경로 설정", Category: CategoryContext, Importance: 0.6})
	_, _ = s.InsertFact(ctx, Fact{Content: "Docker 설정 완료", Category: CategoryContext, Importance: 0.5})

	eid, _ := s.UpsertEntity(ctx, "SGLang", EntityTool)
	_ = s.LinkFactEntity(ctx, id1, eid, "subject")
	_ = s.LinkFactEntity(ctx, id2, eid, "subject")

	facts, err := s.GetFactsByEntity(ctx, "SGLang")
	if err != nil {
		t.Fatalf("GetFactsByEntity: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts for SGLang, got %d", len(facts))
	}
}

func TestGetEntityNetwork(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	f1, _ := s.InsertFact(ctx, Fact{Content: "SGLang + Docker 조합", Category: CategoryContext, Importance: 0.6})
	f2, _ := s.InsertFact(ctx, Fact{Content: "SGLang + Docker 배포", Category: CategoryContext, Importance: 0.6})

	eidSGLang, _ := s.UpsertEntity(ctx, "SGLang", EntityTool)
	eidDocker, _ := s.UpsertEntity(ctx, "Docker", EntityTool)

	_ = s.LinkFactEntity(ctx, f1, eidSGLang, "subject")
	_ = s.LinkFactEntity(ctx, f1, eidDocker, "subject")
	_ = s.LinkFactEntity(ctx, f2, eidSGLang, "subject")
	_ = s.LinkFactEntity(ctx, f2, eidDocker, "subject")

	network, err := s.GetEntityNetwork(ctx)
	if err != nil {
		t.Fatalf("GetEntityNetwork: %v", err)
	}
	if len(network) != 1 {
		t.Fatalf("expected 1 entity pair, got %d", len(network))
	}
	if network[0].CoOccurrences != 2 {
		t.Errorf("expected 2 co-occurrences, got %d", network[0].CoOccurrences)
	}
}

func TestInferEntityType(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"Go", EntityTool},
		{"SQLite", EntityTool},
		{"Fred/JOCA Cable", EntityProject},
		{"SomeRandomThing", EntityUnknown},
	}
	for _, tc := range tests {
		got := inferEntityType(tc.name)
		if got != tc.expected {
			t.Errorf("inferEntityType(%q) = %q, want %q", tc.name, got, tc.expected)
		}
	}
}
