package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
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

	if !strings.Contains(result, "대화 기억") && !strings.Contains(result, "관련 지식") {
		t.Fatalf("expected knowledge section, got: %q", result)
	}
	if !strings.Contains(result, "search index repair completed for historical context") {
		t.Fatalf("expected unified message content, got: %q", result)
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
