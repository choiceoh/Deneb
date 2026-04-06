package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
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

func TestPrefetch_UnifiedRecallOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := unified.New(unified.Config{
		DatabasePath: filepath.Join(dir, "deneb.db"),
	}, nil)
	if err != nil {
		t.Fatalf("new unified store: %v", err)
	}
	defer store.Close()

	_, err = store.DB().Exec(
		`INSERT INTO messages (message_id, conversation_id, seq, role, content, token_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 1, 1, "assistant", "search index repair completed for historical context", 12, time.Now().Add(-2*time.Hour).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	result := Prefetch(context.Background(), "search index repair", Deps{
		UnifiedStore: store,
	})

	if !strings.Contains(result, "대화 기억") {
		t.Fatalf("expected unified recall section, got: %q", result)
	}
	if !strings.Contains(result, "search index repair completed for historical context") {
		t.Fatalf("expected unified message content, got: %q", result)
	}
}

func TestPrefetch_StructuredFactsDoNotDuplicateUnifiedFacts(t *testing.T) {
	dir := t.TempDir()
	store, err := unified.New(unified.Config{
		DatabasePath: filepath.Join(dir, "deneb.db"),
	}, nil)
	if err != nil {
		t.Fatalf("new unified store: %v", err)
	}
	defer store.Close()

	memStore, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("new memory store: %v", err)
	}

	content := "prefer concise code review summaries for follow-ups"
	if _, err := memStore.InsertFact(context.Background(), memory.Fact{
		Content:    content,
		Category:   memory.CategoryPreference,
		Importance: 0.95,
		Source:     memory.SourceManual,
	}); err != nil {
		t.Fatalf("insert fact: %v", err)
	}

	result := Prefetch(context.Background(), "concise code review summaries", Deps{
		MemoryStore:  memStore,
		UnifiedStore: store,
	})

	if count := strings.Count(result, content); count != 1 {
		t.Fatalf("expected fact to appear once, got %d in %q", count, result)
	}
}

func TestFormatKnowledge_TokenBudget(t *testing.T) {
	// Create memory results that would exceed the token budget.
	var facts []memory.SearchResult
	longContent := strings.Repeat("가나다라마바사 ", 200)
	for i := 0; i < 20; i++ {
		facts = append(facts, memory.SearchResult{
			Fact: memory.Fact{
				Content:    longContent,
				Category:   "context",
				Importance: 0.5,
			},
			Score: 0.5,
		})
	}

	formatted := formatKnowledgeWithFacts(nil, nil, facts, nil)
	tokens := len(formatted) / charsPerToken
	if tokens > knowledgeMaxTokens+500 { // allow small overshoot from last item
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

func TestFormatMutualUnderstanding(t *testing.T) {
	t.Run("empty entries", func(t *testing.T) {
		result := formatMutualUnderstanding(nil)
		if result != "" {
			t.Errorf("expected empty for nil entries, got %q", result)
		}
	})

	t.Run("profile only", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "communication_style", Value: "간결한 답변 선호"},
		}
		result := formatMutualUnderstanding(entries)
		if !strings.Contains(result, "소통 스타일") || !strings.Contains(result, "간결한 답변") {
			t.Errorf("expected profile content, got %q", result)
		}
		if !strings.Contains(result, "사용자 프로필") {
			t.Errorf("expected profile header, got %q", result)
		}
	})

	t.Run("mutual keys", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "user_sees_ai", Value: "높은 신뢰"},
			{Key: "adaptation_notes", Value: "코드 리뷰는 자세하게"},
		}
		result := formatMutualUnderstanding(entries)
		if !strings.Contains(result, "사용자 → AI 인식") || !strings.Contains(result, "높은 신뢰") {
			t.Errorf("expected user_sees_ai content, got %q", result)
		}
		if !strings.Contains(result, "적응 메모") {
			t.Errorf("expected adaptation_notes header, got %q", result)
		}
	})

	t.Run("recent signals", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "mu_signals_raw", Value: "[satisfaction:strong] 잘했어\n[correction:mild] 좀 더 짧게"},
			{Key: "user_sees_ai", Value: "만족"},
		}
		result := formatMutualUnderstanding(entries)
		if !strings.Contains(result, "최근 시그널") {
			t.Errorf("expected recent signals section, got %q", result)
		}
	})

	t.Run("guidance section", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "user_sees_ai", Value: "test"},
		}
		result := formatMutualUnderstanding(entries)
		if !strings.Contains(result, "활용 지침") || !strings.Contains(result, "최우선") {
			t.Errorf("expected guidance with priority framework, got %q", result)
		}
	})

	t.Run("history section", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "mu_history", Value: "[03-27] 신뢰 수준 상승"},
			{Key: "user_sees_ai", Value: "test"},
		}
		result := formatMutualUnderstanding(entries)
		if !strings.Contains(result, "관계 변화 이력") {
			t.Errorf("expected history section, got %q", result)
		}
	})

	t.Run("skips empty mu_signals_raw", func(t *testing.T) {
		entries := []memory.UserModelEntry{
			{Key: "mu_signals_raw", Value: ""},
			{Key: "user_sees_ai", Value: "test"},
		}
		result := formatMutualUnderstanding(entries)
		if strings.Contains(result, "최근 시그널 (미통합)") {
			t.Errorf("should not show recent signals section for empty raw, got %q", result)
		}
	})
}

func TestRelativeTimeSince(t *testing.T) {
	now := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"future", now.Add(time.Hour), "방금"},
		{"30 seconds ago", now.Add(-30 * time.Second), "방금"},
		{"59 minutes ago", now.Add(-59 * time.Minute), "방금"},
		{"1 hour ago", now.Add(-time.Hour), "1시간 전"},
		{"3 hours ago", now.Add(-3 * time.Hour), "3시간 전"},
		{"23 hours ago", now.Add(-23 * time.Hour), "23시간 전"},
		{"1 day ago", now.Add(-25 * time.Hour), "어제"},
		{"2 days ago", now.Add(-2 * 24 * time.Hour), "그저께"},
		{"3 days ago", now.Add(-3 * 24 * time.Hour), "3일 전"},
		{"6 days ago", now.Add(-6 * 24 * time.Hour), "6일 전"},
		{"1 week ago", now.Add(-8 * 24 * time.Hour), "1주 전"},
		{"3 weeks ago", now.Add(-22 * 24 * time.Hour), "3주 전"},
		{"1 month ago", now.Add(-35 * 24 * time.Hour), "1개월 전"},
		{"6 months ago", now.Add(-180 * 24 * time.Hour), "6개월 전"},
		{"1 year ago", now.Add(-400 * 24 * time.Hour), "1년 전"},
		{"2 years ago", now.Add(-730 * 24 * time.Hour), "2년 전"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relativeTimeSince(tt.t, now)
			if got != tt.want {
				t.Errorf("relativeTimeSince(%v, now) = %q, want %q", tt.t, got, tt.want)
			}
		})
	}
}

func TestVolatileHint(t *testing.T) {
	now := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		category string
		age      time.Duration
		want     string
	}{
		{"context fresh", "context", 15 * 24 * time.Hour, ""},
		{"context needs verification (50%)", "context", 25 * 24 * time.Hour, "확인 필요"},
		{"context past shelf life", "context", 50 * 24 * time.Hour, "⚠변경 가능"},
		{"preference fresh", "preference", 30 * 24 * time.Hour, ""},
		{"preference needs verification", "preference", 60 * 24 * time.Hour, "확인 필요"},
		{"preference past shelf life", "preference", 100 * 24 * time.Hour, "⚠변경 가능"},
		{"decision past shelf life", "decision", 140 * 24 * time.Hour, "⚠변경 가능"},
		{"decision needs verification", "decision", 80 * 24 * time.Hour, "확인 필요"},
		{"unknown category default 60d", "unknown", 70 * 24 * time.Hour, "⚠변경 가능"},
		{"zero time", "context", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedAt := now.Add(-tt.age)
			if tt.name == "zero time" {
				updatedAt = time.Time{}
			}
			got := volatileHint(tt.category, updatedAt, now)
			if got != tt.want {
				t.Errorf("volatileHint(%q, age=%v) = %q, want %q", tt.category, tt.age, got, tt.want)
			}
		})
	}
}

func TestFactTemporalAnnotation(t *testing.T) {
	now := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	t.Run("simple recent fact", func(t *testing.T) {
		f := memory.Fact{
			Category:  "decision",
			CreatedAt: now.Add(-3 * 24 * time.Hour),
			UpdatedAt: now.Add(-3 * 24 * time.Hour),
		}
		got := factTemporalAnnotation(f, now)
		if got != "3일 전" {
			t.Errorf("got %q, want %q", got, "3일 전")
		}
	})

	t.Run("created/updated gap shows both", func(t *testing.T) {
		f := memory.Fact{
			Category:  "context",
			CreatedAt: now.Add(-90 * 24 * time.Hour), // 3 months ago
			UpdatedAt: now.Add(-1 * 24 * time.Hour),  // yesterday
		}
		got := factTemporalAnnotation(f, now)
		if !strings.Contains(got, "갱신:") {
			t.Errorf("expected 갱신: separator for large gap, got %q", got)
		}
		if !strings.Contains(got, "어제") {
			t.Errorf("expected '어제' for updated, got %q", got)
		}
	})

	t.Run("small gap uses single time", func(t *testing.T) {
		f := memory.Fact{
			Category:  "decision",
			CreatedAt: now.Add(-5 * 24 * time.Hour),
			UpdatedAt: now.Add(-3 * 24 * time.Hour),
		}
		got := factTemporalAnnotation(f, now)
		if strings.Contains(got, "갱신:") {
			t.Errorf("should not show 갱신 for small gap, got %q", got)
		}
	})

	t.Run("stale context shows volatility", func(t *testing.T) {
		f := memory.Fact{
			Category:  "context",
			CreatedAt: now.Add(-90 * 24 * time.Hour),
			UpdatedAt: now.Add(-50 * 24 * time.Hour), // past 45-day shelf life
		}
		got := factTemporalAnnotation(f, now)
		if !strings.Contains(got, "⚠변경 가능") {
			t.Errorf("expected volatility hint, got %q", got)
		}
	})

	t.Run("stable preference no volatility", func(t *testing.T) {
		f := memory.Fact{
			Category:  "preference",
			CreatedAt: now.Add(-30 * 24 * time.Hour),
			UpdatedAt: now.Add(-30 * 24 * time.Hour),
		}
		got := factTemporalAnnotation(f, now)
		if strings.Contains(got, "⚠변경 가능") {
			t.Errorf("preference within shelf life should not be volatile, got %q", got)
		}
	})

	t.Run("zero times returns empty", func(t *testing.T) {
		f := memory.Fact{Category: "context"}
		got := factTemporalAnnotation(f, now)
		if got != "" {
			t.Errorf("expected empty for zero times, got %q", got)
		}
	})
}

func TestFormatKnowledgeWithFacts_TemporalAnnotation(t *testing.T) {
	now := time.Now()

	t.Run("shows temporal label", func(t *testing.T) {
		facts := []memory.SearchResult{{
			Fact: memory.Fact{
				Content:    "Go 1.22로 업그레이드 결정",
				Category:   "decision",
				Importance: 0.8,
				CreatedAt:  now.Add(-3 * 24 * time.Hour),
				UpdatedAt:  now.Add(-3 * 24 * time.Hour),
			},
			Score: 0.9,
		}}
		result := formatKnowledgeWithFacts(nil, nil, facts, nil)
		if !strings.Contains(result, "(3일 전)") {
			t.Errorf("expected temporal label '(3일 전)', got: %q", result)
		}
	})

	t.Run("shows created/updated separation", func(t *testing.T) {
		facts := []memory.SearchResult{{
			Fact: memory.Fact{
				Content:    "test fact",
				Category:   "context",
				Importance: 0.5,
				CreatedAt:  now.Add(-90 * 24 * time.Hour), // 3 months ago
				UpdatedAt:  now.Add(-2 * time.Hour),       // 2 hours ago
			},
			Score: 0.8,
		}}
		result := formatKnowledgeWithFacts(nil, nil, facts, nil)
		if !strings.Contains(result, "갱신:") {
			t.Errorf("expected created/updated separation, got: %q", result)
		}
		if !strings.Contains(result, "2시간 전") {
			t.Errorf("expected '2시간 전' from UpdatedAt, got: %q", result)
		}
	})

	t.Run("graceful degradation for zero time", func(t *testing.T) {
		facts := []memory.SearchResult{{
			Fact: memory.Fact{
				Content:    "no timestamp fact",
				Category:   "context",
				Importance: 0.5,
			},
			Score: 0.7,
		}}
		result := formatKnowledgeWithFacts(nil, nil, facts, nil)
		if strings.Contains(result, "()") {
			t.Errorf("should not show empty parens, got: %q", result)
		}
		if !strings.Contains(result, "no timestamp fact") {
			t.Errorf("fact content should still appear, got: %q", result)
		}
	})
}
