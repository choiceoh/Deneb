package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "test_memory.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndGetFact(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id, err := s.InsertFact(ctx, Fact{
		Content:    "Podman을 Docker 대신 사용하기로 결정",
		Category:   CategoryDecision,
		Importance: 0.9,
	})
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	f, err := s.GetFact(ctx, id)
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if f.Content != "Podman을 Docker 대신 사용하기로 결정" {
		t.Errorf("unexpected content: %s", f.Content)
	}
	if f.Category != CategoryDecision {
		t.Errorf("unexpected category: %s", f.Category)
	}
	if f.Importance != 0.9 {
		t.Errorf("unexpected importance: %f", f.Importance)
	}
	if !f.Active {
		t.Error("expected active=true")
	}
	if f.AccessCount != 1 {
		t.Errorf("expected access_count=1, got %d", f.AccessCount)
	}
}

func TestFTSSearch(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_, _ = s.InsertFact(ctx, Fact{Content: "Rust와 Go를 FFI로 연결", Category: CategoryContext, Importance: 0.7})
	_, _ = s.InsertFact(ctx, Fact{Content: "SGLang 서버 포트 30000 사용", Category: CategoryDecision, Importance: 0.8})
	_, _ = s.InsertFact(ctx, Fact{Content: "한국어를 기본 언어로 사용", Category: CategoryPreference, Importance: 0.9})

	results, err := s.SearchFacts(ctx, "SGLang", nil, SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result for 'SGLang'")
	}
	if results[0].Fact.Content != "SGLang 서버 포트 30000 사용" {
		t.Errorf("unexpected top result: %s", results[0].Fact.Content)
	}
}

func TestDeactivateAndSupersede(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, _ := s.InsertFact(ctx, Fact{Content: "fact A", Category: CategoryContext, Importance: 0.5})
	id2, _ := s.InsertFact(ctx, Fact{Content: "fact B (merged)", Category: CategoryContext, Importance: 0.7})

	if err := s.SupersedeFact(ctx, id1, id2); err != nil {
		t.Fatalf("SupersedeFact: %v", err)
	}

	f, _ := s.GetFact(ctx, id1)
	if f.Active {
		t.Error("expected fact to be inactive after supersede")
	}
	if f.SupersededBy == nil || *f.SupersededBy != id2 {
		t.Error("expected superseded_by to be set")
	}
}

func TestExportToMarkdown(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_, _ = s.InsertFact(ctx, Fact{Content: "결정 사항", Category: CategoryDecision, Importance: 0.9})
	_, _ = s.InsertFact(ctx, Fact{Content: "선호도 사항", Category: CategoryPreference, Importance: 0.8})

	md, err := s.ExportToMarkdown(ctx)
	if err != nil {
		t.Fatalf("ExportToMarkdown: %v", err)
	}
	if md == "" {
		t.Fatal("expected non-empty markdown export")
	}
	if !contains(md, "결정사항") {
		t.Error("expected '결정사항' section header")
	}
	if !contains(md, "결정 사항") {
		t.Error("expected fact content in export")
	}
}

func TestExportToFile(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_, _ = s.InsertFact(ctx, Fact{Content: "테스트 팩트", Category: CategoryContext, Importance: 0.5})

	dir := t.TempDir()
	if err := s.ExportToFile(ctx, dir); err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !contains(string(data), "테스트 팩트") {
		t.Error("expected fact content in MEMORY.md")
	}
}

func TestUserModel(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	if err := s.SetUserModel(ctx, "expertise_areas", "Rust, Go, 시스템 프로그래밍", 0.9); err != nil {
		t.Fatalf("SetUserModel: %v", err)
	}

	entries, err := s.GetUserModel(ctx)
	if err != nil {
		t.Fatalf("GetUserModel: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Key != "expertise_areas" || entries[0].Value != "Rust, Go, 시스템 프로그래밍" {
		t.Errorf("unexpected entry: %+v", entries[0])
	}
}

func TestMetadata(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_ = s.SetMeta(ctx, "turn_count", "42")
	v, _ := s.GetMeta(ctx, "turn_count")
	if v != "42" {
		t.Errorf("expected '42', got %q", v)
	}
}

func TestEmbeddingStorage(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id, _ := s.InsertFact(ctx, Fact{Content: "embed test", Category: CategoryContext, Importance: 0.5})

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := s.StoreEmbedding(ctx, id, vec, "test-model"); err != nil {
		t.Fatalf("StoreEmbedding: %v", err)
	}

	embeddings, err := s.LoadEmbeddings(ctx)
	if err != nil {
		t.Fatalf("LoadEmbeddings: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(embeddings))
	}
	loaded := embeddings[id]
	if len(loaded) != 4 {
		t.Fatalf("expected 4-dim vector, got %d", len(loaded))
	}
	for i, v := range vec {
		if loaded[i] != v {
			t.Errorf("embedding[%d]: expected %f, got %f", i, v, loaded[i])
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if sim := cosineSimilarity(a, b); sim < 0.99 {
		t.Errorf("identical vectors should have similarity ~1, got %f", sim)
	}

	c := []float32{0, 1, 0}
	if sim := cosineSimilarity(a, c); sim > 0.01 {
		t.Errorf("orthogonal vectors should have similarity ~0, got %f", sim)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
