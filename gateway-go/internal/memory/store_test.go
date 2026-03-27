package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if !strings.Contains(md, "결정사항") {
		t.Error("expected '결정사항' section header")
	}
	if !strings.Contains(md, "결정 사항") {
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
	if !strings.Contains(string(data), "테스트 팩트") {
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

func TestGetUserModelEntry(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	// Non-existent key returns nil, nil.
	entry, err := s.GetUserModelEntry(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for missing key, got %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil entry for missing key, got %+v", entry)
	}

	// Insert and retrieve.
	_ = s.SetUserModel(ctx, "mu_test", "테스트 값", 0.75)
	entry, err = s.GetUserModelEntry(ctx, "mu_test")
	if err != nil {
		t.Fatalf("GetUserModelEntry: %v", err)
	}
	if entry == nil || entry.Value != "테스트 값" || entry.Confidence != 0.75 {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestGetFactReadOnly(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id, _ := s.InsertFact(ctx, Fact{
		Content:  "읽기 전용 테스트",
		Category: CategoryContext,
	})

	// ReadOnly should not increment access_count.
	f, err := s.GetFactReadOnly(ctx, id)
	if err != nil {
		t.Fatalf("GetFactReadOnly: %v", err)
	}
	if f.AccessCount != 0 {
		t.Errorf("expected access_count=0 after ReadOnly, got %d", f.AccessCount)
	}

	// Regular GetFact should increment.
	f2, _ := s.GetFact(ctx, id)
	if f2.AccessCount != 1 {
		t.Errorf("expected access_count=1 after GetFact, got %d", f2.AccessCount)
	}
}

func TestFormatPreviousState(t *testing.T) {
	empty := formatPreviousState(map[string]string{})
	if empty != "" {
		t.Errorf("expected empty string for empty map, got %q", empty)
	}

	m := map[string]string{
		"user_sees_ai":    "만족도 높음",
		"adaptation_notes": "간결하게 답변할 것",
	}
	result := formatPreviousState(m)
	if !strings.Contains(result, "사용자 → AI 인식") || !strings.Contains(result, "만족도 높음") {
		t.Errorf("expected user_sees_ai label+value, got %q", result)
	}
	if !strings.Contains(result, "적응 메모") || !strings.Contains(result, "간결하게") {
		t.Errorf("expected adaptation_notes label+value, got %q", result)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	// Korean text: each character is 3 bytes but 1 rune.
	korean := "가나다라마바사아자차카타파하"
	result := truncate(korean, 5)
	if result != "가나다라마..." {
		t.Errorf("expected 5 Korean runes + ellipsis, got %q", result)
	}

	// Short string should pass through unchanged.
	short := "abc"
	if truncate(short, 10) != "abc" {
		t.Errorf("short string should pass through unchanged")
	}
}

