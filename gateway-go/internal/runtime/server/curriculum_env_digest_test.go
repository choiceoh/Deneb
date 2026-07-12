package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// truncRunes caps at n runes with an ellipsis.
func TestTruncRunes(t *testing.T) {
	if got := truncRunes("abc", 5); got != "abc" {
		t.Fatalf("short string should be unchanged: got %q", got)
	}
	got := truncRunes("abcdef", 4)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 4 {
		t.Fatalf("truncRunes(6,4) = %q, want 4 runes ending …", got)
	}
	// Korean runes count as 1 each.
	got = truncRunes("한글테스트데이터", 5)
	if len([]rune(got)) != 5 {
		t.Fatalf("Korean truncRunes = %q, want 5 runes", got)
	}
}

// The digest surfaces recent feed-item titles when a workfeed store is wired.
func TestCurriculumEnvDigest_FeedItems(t *testing.T) {
	dir := t.TempDir()
	store := workfeed.NewStore(filepath.Join(dir, "workfeed.jsonl"))
	for _, title := range []string{"계약 검토 — NDA 초안", "주간 보고서 작성", "회의록 정리"} {
		if _, err := store.Append(workfeed.Item{Source: "test", Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{MemorySubsystem: &MemorySubsystem{workFeedStore: store}}
	got := s.curriculumEnvDigest(context.Background())
	if got == "" {
		t.Fatal("expected digest with feed items, got empty")
	}
	if !strings.Contains(got, "계약 검토") {
		t.Fatalf("digest missing feed title:\n%s", got)
	}
	if !strings.Contains(got, "업무 피드") {
		t.Fatalf("digest missing section header:\n%s", got)
	}
}

// No stores wired (dev/test) → empty digest, quiet.
func TestCurriculumEnvDigest_Empty(t *testing.T) {
	s := &Server{MemorySubsystem: &MemorySubsystem{}}
	if got := s.curriculumEnvDigest(context.Background()); got != "" {
		t.Fatalf("empty stores should yield empty digest, got %q", got)
	}
}
