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

// TestGraphContext_InlineWikiLinks verifies that an Obsidian-style [[wiki-link]]
// written in a page body becomes a graph edge even when there is no matching
// `related:` frontmatter entry — the loop the dreamer's emitted links left open.
func TestGraphContext_InlineWikiLinks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// The deal page links to the person purely via an inline [[...]] in prose —
	// no Related[], no shared tag. Only the inline-link pass can connect them.
	mustWrite(t, store, "people/honggildong.md", &Page{
		Meta: Frontmatter{ID: "honggildong", Title: "홍길동", Category: "사람", Summary: "탑솔라 구매 담당"},
		Body: "탑솔라 거래의 핵심 의사결정권자.",
	})
	mustWrite(t, store, "deals/topsolar.md", &Page{
		Meta: Frontmatter{ID: "topsolar", Title: "탑솔라 거래", Category: "거래", Summary: "연 5억 공급 계약"},
		Body: "발주 담당자는 [[people/honggildong]] 부장.\n\n## 관련 문서\n- [[people/honggildong.md|홍길동]]\n",
	})
	mustWrite(t, store, "tech/dgx.md", &Page{
		Meta: Frontmatter{ID: "dgx", Title: "DGX Spark", Category: "기술", Summary: "로컬 추론 서버"},
		Body: "GPU 추론.",
	})

	ctx := context.Background()
	got, err := store.GraphContext(ctx, "탑솔라 거래", 8)
	if err != nil {
		t.Fatalf("GraphContext: %v", err)
	}
	if !strings.Contains(got, "홍길동") {
		t.Errorf("inline [[wiki-link]] neighbor missing:\n%s", got)
	}
	if !strings.Contains(got, "링크") {
		t.Errorf("expected the neighbor to be labeled as a link edge:\n%s", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("unrelated page leaked into neighbors:\n%s", got)
	}
}

func TestExtractWikiLinks(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"none", "plain prose with no links", nil},
		{"simple", "see [[dgx-spark]] for details", []string{"dgx-spark"}},
		{"alias", "owner is [[people/honggildong.md|홍길동]]", []string{"people/honggildong.md"}},
		{"section", "per [[운영시스템/배포#롤백]] section", []string{"운영시스템/배포"}},
		{"dedup", "[[a]] then [[a]] again and [[b]]", []string{"a", "b"}},
		{"multi-line", "- [[프로젝트/x]]\n- [[프로젝트/y]]\n", []string{"프로젝트/x", "프로젝트/y"}},
		{"empty-target", "[[]] and [[ | alias]]", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractWikiLinks(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("ExtractWikiLinks(%q) = %v, want %v", tc.body, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ExtractWikiLinks(%q)[%d] = %q, want %q", tc.body, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestPageConnections verifies the compact neighbor footer seeds by exact path
// and lists strongest neighbors, returning "" for an isolated page.
func TestPageConnections(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "people/honggildong.md", &Page{
		Meta: Frontmatter{ID: "honggildong", Title: "홍길동", Category: "사람", Summary: "탑솔라 구매 담당"},
		Body: "핵심 담당자.",
	})
	mustWrite(t, store, "deals/topsolar.md", &Page{
		Meta: Frontmatter{ID: "topsolar", Title: "탑솔라 거래", Category: "거래", Related: []string{"people/honggildong.md"}},
		Body: "발주 검토 중. 참고: [[people/honggildong]].",
	})
	mustWrite(t, store, "tech/dgx.md", &Page{
		Meta: Frontmatter{ID: "dgx", Title: "DGX Spark", Category: "기술"},
		Body: "GPU.",
	})

	ctx := context.Background()
	got, err := store.PageConnections(ctx, "deals/topsolar.md", 6)
	if err != nil {
		t.Fatalf("PageConnections: %v", err)
	}
	if !strings.Contains(got, "홍길동") {
		t.Errorf("expected 홍길동 neighbor in footer, got: %q", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("isolated page leaked into footer: %q", got)
	}

	// Isolated page → empty footer.
	if got, _ := store.PageConnections(ctx, "tech/dgx.md", 6); got != "" {
		t.Errorf("expected empty footer for isolated page, got: %q", got)
	}
}

func mustWrite(t *testing.T, store *Store, relPath string, page *Page) {
	t.Helper()
	if err := store.WritePage(relPath, page); err != nil {
		t.Fatalf("WritePage %s: %v", relPath, err)
	}
}
