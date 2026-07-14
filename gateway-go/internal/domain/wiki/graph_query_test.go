package wiki

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphContextReturnsMatchingNeighbors(t *testing.T) {
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
	// Neighbors are labeled by what the target IS (its category), not by how
	// the edge was found.
	if !strings.Contains(got, "탑솔라 거래 (거래)") {
		t.Errorf("neighbor should carry its semantic kind label:\n%s", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("unrelated page leaked into neighbors:\n%s", got)
	}

	// Unknown name → empty (no hallucinated match).
	if got, _ := store.GraphContext(ctx, "존재하지않는사람", 8); got != "" {
		t.Errorf("expected empty for unknown query, got: %s", got)
	}
}

// TestGraphContext_InlineWikiLinksCreateEdges verifies that an Obsidian-style
// [[wiki-link]] written in a page body becomes a graph edge even when there is
// no matching `related:` frontmatter entry — the loop the dreamer's emitted
// links left open.
func TestGraphContext_InlineWikiLinksCreateEdges(t *testing.T) {
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
	// The inline link forms the edge; the rendered label is the target's kind.
	if !strings.Contains(got, "홍길동 (사람)") {
		t.Errorf("expected the link neighbor labeled by its kind (사람):\n%s", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("unrelated page leaked into neighbors:\n%s", got)
	}
}

func TestExtractWikiLinksParsesLinkForms(t *testing.T) {
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

// TestPageConnectionsReturnsNeighborFooter verifies the compact neighbor
// footer seeds by exact path and lists strongest neighbors, returning "" for
// an isolated page.
func TestPageConnectionsReturnsNeighborFooter(t *testing.T) {
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
	// Root-level page with no category: no semantic kind, so the mechanism
	// label (관련) must survive as the fallback.
	mustWrite(t, store, "memo.md", &Page{
		Meta: Frontmatter{ID: "memo", Title: "메모장", Related: []string{"deals/topsolar.md"}},
		Body: "잡다한 기록.",
	})

	ctx := context.Background()
	got, err := store.PageConnections(ctx, "deals/topsolar.md", 6)
	if err != nil {
		t.Fatalf("PageConnections: %v", err)
	}
	if !strings.Contains(got, "홍길동(사람)") {
		t.Errorf("expected 홍길동 neighbor labeled by kind in footer, got: %q", got)
	}
	if !strings.Contains(got, "메모장(관련)") {
		t.Errorf("expected mechanism-label fallback for unclassifiable page, got: %q", got)
	}
	if strings.Contains(got, "DGX Spark") {
		t.Errorf("isolated page leaked into footer: %q", got)
	}

	// Isolated page → empty footer.
	if got, _ := store.PageConnections(ctx, "tech/dgx.md", 6); got != "" {
		t.Errorf("expected empty footer for isolated page, got: %q", got)
	}
}

// TestGraphContext_ProjectFamilyCreatesEdges verifies that pages sharing a
// project folder connect with no explicit link (the 2026-07 orphan class:
// 로그/기자재 invisible to GraphContext), and that raw mail-analysis pages join
// weaker than curated slots.
func TestGraphContext_ProjectFamilyCreatesEdges(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/영산고/대표.md", &Page{
		Meta: Frontmatter{Title: "영산고", Summary: "지붕 태양광"},
		Body: "개요.",
	})
	mustWrite(t, store, "프로젝트/영산고/로그.md", &Page{
		Meta: Frontmatter{Title: "진행 기록"},
		Body: "2026-06 착공.",
	})
	mustWrite(t, store, "프로젝트/영산고/기자재/케이블.md", &Page{
		Meta: Frontmatter{Title: "XLPE 케이블"},
		Body: "500m 발주분.",
	})
	mustWrite(t, store, "프로젝트/영산고/메일분석/m1.md", &Page{
		Meta: Frontmatter{Title: "발주 확인 요청"},
		Body: "수신 확인 바랍니다.",
	})
	// Another project's page must not join the family.
	mustWrite(t, store, "프로젝트/무안군청/대표.md", &Page{
		Meta: Frontmatter{Title: "무안군청", Summary: "관공서 발주"},
		Body: "별건.",
	})
	// Archived pages (rotated log archives) never surface as neighbors.
	mustWrite(t, store, "프로젝트/영산고/로그-보관.md", &Page{
		Meta: Frontmatter{Title: "옛 진행 기록", Archived: true},
		Body: "오래된 로그.",
	})

	got, err := store.GraphContext(context.Background(), "영산고", 8)
	if err != nil {
		t.Fatalf("GraphContext: %v", err)
	}
	for _, want := range []string{"진행 기록 (로그)", "XLPE 케이블 (기자재)", "발주 확인 요청 (메일)"} {
		if !strings.Contains(got, want) {
			t.Errorf("family neighbor %q missing:\n%s", want, got)
		}
	}
	if strings.Contains(got, "무안군청") {
		t.Errorf("other project leaked into family:\n%s", got)
	}
	if strings.Contains(got, "옛 진행 기록") {
		t.Errorf("archived page resurfaced as a neighbor:\n%s", got)
	}
	// Curated slots (1.0) rank above raw mail analyses (0.6).
	if strings.Index(got, "XLPE 케이블") > strings.Index(got, "발주 확인 요청") {
		t.Errorf("mail analysis outranked curated slot:\n%s", got)
	}
}

// TestGraphScoreMap_PreservesPhasePrecedenceAndBoundaries locks the orchestration
// seams used by the named edge helpers: equal-score authored edges keep the
// earlier Related relation, mention scoring is opt-in, and cancellation stops
// record loading before a partial graph is returned.
func TestGraphScoreMap_PreservesPhasePrecedenceAndBoundaries(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, store, "seed.md", &Page{
		Meta: Frontmatter{Title: "시드", Related: []string{"target.md"}},
		Body: "[[target.md]]와 언급 후보를 함께 본다.",
	})
	mustWrite(t, store, "target.md", &Page{
		Meta: Frontmatter{Title: "타깃"},
		Body: "명시 연결 대상",
	})
	mustWrite(t, store, "mention.md", &Page{
		Meta: Frontmatter{Title: "언급 후보"},
		Body: "본문 언급으로만 연결되는 대상",
	})

	find := func(recs []graphRec, path string) int {
		t.Helper()
		for i := range recs {
			if recs[i].relPath == path {
				return i
			}
		}
		t.Fatalf("record %q not found in %+v", path, recs)
		return -1
	}

	recs, seed, withoutMentions, err := store.graphScoreMap(
		context.Background(), "", false, "seed.md",
	)
	if err != nil || seed != find(recs, "seed.md") {
		t.Fatalf("seed resolution failed: seed=%d err=%v", seed, err)
	}
	target := find(recs, "target.md")
	if edge := withoutMentions[target]; edge == nil || edge.score != 1.0 || edge.relation != "관련" {
		t.Fatalf("equal-score link must not replace earlier Related edge: %+v", edge)
	}
	mention := find(recs, "mention.md")
	if edge := withoutMentions[mention]; edge != nil {
		t.Fatalf("mention edge present while disabled: %+v", edge)
	}

	recs, _, withMentions, err := store.graphScoreMap(
		context.Background(), "", true, "seed.md",
	)
	if err != nil {
		t.Fatal(err)
	}
	target = find(recs, "target.md")
	if edge := withMentions[target]; edge == nil || edge.relation != "관련" {
		t.Fatalf("later phases must preserve the strongest earlier relation: %+v", edge)
	}
	mention = find(recs, "mention.md")
	if edge := withMentions[mention]; edge == nil || edge.score != 0.7 || edge.relation != "언급" {
		t.Fatalf("enabled forward mention edge = %+v, want score 0.7 relation 언급", edge)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := store.graphScoreMap(ctx, "", true, "seed.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load error = %v, want context.Canceled", err)
	}
}

// TestSemanticNeighborLabelReturnsKind pins the deterministic path/category →
// kind rules: 프로젝트/ layout slots are authoritative, other pages fall back to
// category then top-level folder, and meaningless kinds (기타, root files)
// return "".
func TestSemanticNeighborLabelReturnsKind(t *testing.T) {
	cases := []struct {
		path     string
		category string
		want     string
	}{
		{"프로젝트/거래/한화큐셀.md", "", "거래처"},
		{"프로젝트/기아-화성/대표.md", "", "프로젝트"},
		{"프로젝트/기아-화성.md", "", "프로젝트"},           // legacy flat rep page
		{"프로젝트/기아-화성/케이블 견적 비교.md", "", "프로젝트"}, // detail page
		{"프로젝트/기아-화성/기자재/XLPE 케이블.md", "", "기자재"},
		{"프로젝트/기아-화성/로그.md", "", "로그"},
		{"프로젝트/기아-화성/메일분석/m1.md", "", "메일"},
		{"프로젝트/메일분석/m2.md", "", "메일"},          // unlinked bucket
		{"프로젝트/mail-analyses/m3.md", "", "메일"}, // legacy bucket
		{"인물/김민준.md", "", "인물"},                // top-level folder fallback
		{"people/hong.md", "사람", "사람"},         // category beats foreign folder name
		{"규정/발주.md", "w:규정", "규정"},             // wikilink-leaked category normalized
		{"업무/결재.md", "프로젝트/영산고", "프로젝트"},       // path category → first segment
		{"기타/잡동사니.md", "기타", ""},               // 기타 carries no meaning
		{"index.md", "", ""},                   // root file
	}
	for _, tc := range cases {
		got := semanticNeighborLabel(graphRec{relPath: tc.path, category: tc.category})
		if got != tc.want {
			t.Errorf("semanticNeighborLabel(%q, cat=%q) = %q, want %q", tc.path, tc.category, got, tc.want)
		}
	}
}

func mustWrite(t *testing.T, store *Store, relPath string, page *Page) {
	t.Helper()
	if err := store.WritePage(relPath, page); err != nil {
		t.Fatalf("WritePage %s: %v", relPath, err)
	}
}
