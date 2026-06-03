package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphContext(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// 홍길동(person) — related to the 탑솔라 deal; the deal page mentions 홍길동
	// in its body (reverse mention) and shares a tag.
	mustWrite(t, store, "people/honggildong.md", &Page{
		Meta: Frontmatter{
			ID: "honggildong", Title: "홍길동", Category: "사람",
			Summary: "탑솔라 구매 담당", Tags: []string{"탑솔라"},
			Related: []string{"deals/topsolar.md"},
		},
		Body: "탑솔라 거래의 핵심 의사결정권자.",
	})
	mustWrite(t, store, "deals/topsolar.md", &Page{
		Meta: Frontmatter{
			ID: "topsolar", Title: "탑솔라 거래", Category: "거래",
			Summary: "연 5억 공급 계약", Due: "2026-07-01", Tags: []string{"탑솔라"},
		},
		Body: "홍길동 부장이 발주를 검토 중.",
	})
	// Unrelated page must not show up as a neighbor.
	mustWrite(t, store, "tech/dgx.md", &Page{
		Meta: Frontmatter{ID: "dgx", Title: "DGX Spark", Category: "기술", Summary: "로컬 추론 서버"},
		Body: "GPU 추론.",
	})

	ctx := context.Background()

	// Query by display name from a raw From header — angle-email is stripped.
	got, err := store.GraphContext(ctx, "홍길동 <hong@topsolar.com>", 8)
	if err != nil {
		t.Fatalf("GraphContext: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty context for known person")
	}
	if !strings.Contains(got, "홍길동") || !strings.Contains(got, "탑솔라 구매 담당") {
		t.Errorf("seed summary missing:\n%s", got)
	}
	if !strings.Contains(got, "탑솔라 거래") {
		t.Errorf("related deal missing from neighbors:\n%s", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("unrelated page leaked into neighbors:\n%s", got)
	}

	// Unknown name → empty (no hallucinated match).
	if got, _ := store.GraphContext(ctx, "존재하지않는사람", 8); got != "" {
		t.Errorf("expected empty for unknown query, got: %s", got)
	}
}

func mustWrite(t *testing.T, store *Store, relPath string, page *Page) {
	t.Helper()
	if err := store.WritePage(relPath, page); err != nil {
		t.Fatalf("WritePage %s: %v", relPath, err)
	}
}
