package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	db := openTestDB(t)
	s, err := NewStoreFromDB(db)
	if err != nil {
		t.Fatalf("NewStoreFromDB: %v", err)
	}
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

	results, err := s.SearchFacts(ctx, "SGLang", SearchOpts{Limit: 5})
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

	_, _ = s.InsertFact(ctx, Fact{Content: "테스트 팩트", Category: CategoryContext, Importance: 0.8})

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

func TestExportToMarkdownFiltersLowImportance(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	_, _ = s.InsertFact(ctx, Fact{Content: "노이즈 팩트", Category: CategoryContext, Importance: 0.5})
	_, _ = s.InsertFact(ctx, Fact{Content: "중요한 결정", Category: CategoryDecision, Importance: 0.9})

	md, err := s.ExportToMarkdown(ctx)
	if err != nil {
		t.Fatalf("ExportToMarkdown: %v", err)
	}
	if strings.Contains(md, "노이즈 팩트") {
		t.Error("low-importance fact should not appear in export")
	}
	if !strings.Contains(md, "중요한 결정") {
		t.Error("high-importance fact should appear in export")
	}
}

func TestPruneNoiseFacts(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	// Insert noise: low importance, context, auto_extract, old timestamp.
	noiseID, _ := s.InsertFact(ctx, Fact{
		Content:    "대화 내용 분석 중",
		Category:   CategoryContext,
		Importance: 0.5,
		Source:     SourceAutoExtract,
	})
	// Backdate the noise fact to 10 days ago.
	tenDaysAgo := time.Now().Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx, `UPDATE facts SET created_at = ? WHERE id = ?`, tenDaysAgo, noiseID)

	// Insert valuable fact: high importance decision.
	_, _ = s.InsertFact(ctx, Fact{
		Content:    "Podman 사용 결정",
		Category:   CategoryDecision,
		Importance: 0.9,
		Source:     SourceAutoExtract,
	})

	// Insert low-importance but manually sourced (should be preserved).
	manualID, _ := s.InsertFact(ctx, Fact{
		Content:    "메모",
		Category:   CategoryContext,
		Importance: 0.5,
		Source:     SourceManual,
	})
	_, _ = s.db.ExecContext(ctx, `UPDATE facts SET created_at = ? WHERE id = ?`, tenDaysAgo, manualID)

	pruned, err := s.PruneNoiseFacts(ctx, 0.6, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("PruneNoiseFacts: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned fact, got %d", pruned)
	}

	// Verify noise is deactivated.
	f, _ := s.GetFact(ctx, noiseID)
	if f.Active {
		t.Error("noise fact should be inactive after pruning")
	}

	// Verify manual fact is still active.
	m, _ := s.GetFact(ctx, manualID)
	if !m.Active {
		t.Error("manual fact should remain active")
	}

	// Verify valuable fact count.
	count, _ := s.ActiveFactCount(ctx)
	if count != 2 {
		t.Errorf("expected 2 active facts, got %d", count)
	}
}

func TestCompactMemory(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	// Insert several noise facts (no age restriction for compact).
	for i := 0; i < 5; i++ {
		_, _ = s.InsertFact(ctx, Fact{
			Content:    fmt.Sprintf("noise %d", i),
			Category:   CategoryContext,
			Importance: 0.5,
			Source:     SourceAutoExtract,
		})
	}
	// Insert a valuable fact.
	_, _ = s.InsertFact(ctx, Fact{
		Content:    "핵심 결정",
		Category:   CategoryDecision,
		Importance: 0.9,
	})

	compacted, err := s.CompactMemory(ctx)
	if err != nil {
		t.Fatalf("CompactMemory: %v", err)
	}
	if compacted != 5 {
		t.Errorf("expected 5 compacted, got %d", compacted)
	}

	count, _ := s.ActiveFactCount(ctx)
	if count != 1 {
		t.Errorf("expected 1 active fact remaining, got %d", count)
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
		"user_sees_ai":     "만족도 높음",
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
