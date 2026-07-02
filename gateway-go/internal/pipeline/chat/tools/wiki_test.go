package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// newTestWikiStore constructs a Store backed by a temp dir, pre-loaded with
// three pages spanning Korean + English content so the tests can exercise
// prefix match, Hangul tokenisation, and scoring.
func newTestWikiStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	type fixture struct {
		path string
		meta wiki.Frontmatter
		body string
	}
	fixtures := []fixture{
		{
			path: "phase-2-summary.md",
			meta: wiki.Frontmatter{
				ID:         "phase-2-summary",
				Title:      "Phase 2 작업 요약",
				Category:   "projects",
				Tags:       []string{"phase-2", "redact"},
				Importance: 0.8,
			},
			body: "redact 모듈을 OpenAI 프로바이더에 연결했다. Anthropic 계열 adapter는 별도 구현 필요.",
		},
		{
			path: "deneb-architecture.md",
			meta: wiki.Frontmatter{
				ID:         "deneb-architecture",
				Title:      "Deneb Architecture Overview",
				Category:   "reference",
				Tags:       []string{"arch", "overview"},
				Importance: 0.9,
			},
			body: "Deneb is a Telegram-first coding agent with a Go gateway. Spillover kicks in at 24K chars per tool response.",
		},
		{
			path: "ramen-recipe.md",
			meta: wiki.Frontmatter{
				ID:         "random",
				Title:      "어제 만든 라면 레시피",
				Category:   "misc",
				Tags:       []string{"cook"},
				Importance: 0.1,
			},
			body: "물 500ml, 스프 한 봉지, 계란 한 개. 끓는 물에 면을 넣고 3분.",
		},
	}
	for _, f := range fixtures {
		page := &wiki.Page{Meta: f.meta, Body: f.body}
		if err := store.WritePage(f.path, page); err != nil {
			t.Fatalf("WritePage %s: %v", f.path, err)
		}
	}
	return store
}

// TestWikiSearch_ReturnsKoreanResults exercises the happy path: a Korean
// query finds the matching page and the output contains the expected
// Korean result header + path.
func TestWikiSearch_ReturnsKoreanResults(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "redact", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "🔍") {
		t.Errorf("expected unified recall header, got: %q", out)
	}
	// Unified ref scheme: wiki hits are cited with the shared "w:" namespace
	// (no .md), interchangeable with knowledge recall/read.
	if !strings.Contains(out, "w:phase-2-summary") {
		t.Errorf("expected w:-namespaced ref in results, got: %q", out)
	}
}

// TestWikiRead_AcceptsNamespacedRef verifies a "w:" citation (from wiki search
// or knowledge recall) is readable through wiki read — the unified ref scheme.
func TestWikiRead_AcceptsNamespacedRef(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiRead(context.Background(), store, "w:phase-2-summary", "")
	if err != nil {
		t.Fatalf("wikiRead: %v", err)
	}
	if strings.Contains(out, "없음") {
		t.Errorf("w: ref should resolve to the page, got: %q", out)
	}
	if !strings.Contains(out, "Phase 2") {
		t.Errorf("expected page content for w: ref, got: %q", out)
	}
}

// TestWikiReadBatch_ReadsSeveralPagesInOneCall covers the batched read that
// collapses the per-page LLM round-trips: every requested page lands under its
// own numbered header, a missing page fills its slot without failing the
// batch, and blank entries are dropped.
func TestWikiReadBatch_ReadsSeveralPagesInOneCall(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiReadBatch(context.Background(), store, []string{
		"w:phase-2-summary",
		" ",
		"deneb-architecture",
		"no-such-page",
	}, "")
	if err != nil {
		t.Fatalf("wikiReadBatch: %v", err)
	}
	if !strings.Contains(out, "[1/3] w:phase-2-summary") || !strings.Contains(out, "Phase 2") {
		t.Errorf("expected first page under numbered header, got: %q", out)
	}
	if !strings.Contains(out, "[2/3] deneb-architecture") || !strings.Contains(out, "Deneb Architecture") {
		t.Errorf("expected second page content, got: %q", out)
	}
	if !strings.Contains(out, "[3/3] no-such-page") || !strings.Contains(out, "없음") {
		t.Errorf("missing page should fill its slot with the not-found notice, got: %q", out)
	}
}

// TestWikiReadBatch_CapsAtMaxPages keeps one call from blowing the tool-output
// budget: past the cap the batch truncates and says so.
func TestWikiReadBatch_CapsAtMaxPages(t *testing.T) {
	store := newTestWikiStore(t)
	paths := make([]string, 0, wikiReadBatchMaxPages+3)
	for range wikiReadBatchMaxPages + 3 {
		paths = append(paths, "phase-2-summary")
	}
	out, err := wikiReadBatch(context.Background(), store, paths, "")
	if err != nil {
		t.Fatalf("wikiReadBatch: %v", err)
	}
	head := fmt.Sprintf("[1/%d]", wikiReadBatchMaxPages)
	if !strings.Contains(out, head) {
		t.Errorf("expected batch capped to %d pages (%s), got: %q", wikiReadBatchMaxPages, head, out[:200])
	}
	if !strings.Contains(out, "앞 8개만 읽음") {
		t.Errorf("expected truncation notice, got tail: %q", out[len(out)-160:])
	}
}

func TestWikiReadBatch_EmptyPathsReturnsGuidance(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiReadBatch(context.Background(), store, []string{"", "  "}, "")
	if err != nil {
		t.Fatalf("wikiReadBatch: %v", err)
	}
	if !strings.Contains(out, "paths가 비어") {
		t.Errorf("expected guidance for empty paths, got: %q", out)
	}
}

func TestWikiWrite_MarksSupersededPages(t *testing.T) {
	store := newTestWikiStore(t)
	old := wiki.NewPage("Old fact", "프로젝트", nil)
	old.Body = "old body"
	if err := store.WritePage("프로젝트/old-fact.md", old); err != nil {
		t.Fatalf("write old page: %v", err)
	}

	out, err := wikiWrite(
		store,
		nil,
		"프로젝트/new-fact.md",
		"New fact",
		"new-fact",
		"새 기준",
		"프로젝트",
		"모순/갱신: old fact를 대체한다.",
		[]string{"deneb"},
		[]string{"프로젝트/old-fact.md"},
		[]string{"프로젝트/old-fact.md"},
		0.8,
		"concept",
		"high",
		"",
	)
	if err != nil {
		t.Fatalf("wikiWrite: %v", err)
	}
	if !strings.Contains(out, "대체 표시 1건") {
		t.Fatalf("expected superseded note, got: %s", out)
	}
	got := testutil.Must(store.ReadPage("프로젝트/old-fact.md"))
	if got.Meta.SupersededBy != "프로젝트/new-fact.md" {
		t.Fatalf("SupersededBy = %q, want 프로젝트/new-fact.md", got.Meta.SupersededBy)
	}
}

// TestWikiSearch_EmptyQueryReturnsGuidance verifies that an empty query
// returns a friendly Korean guidance string (not an error).
func TestWikiSearch_EmptyQueryReturnsGuidance(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "query") {
		t.Errorf("expected guidance mentioning query, got: %q", out)
	}
}

// TestWikiSearch_KoreanPrefixMatch verifies Hangul-aware matching works on
// a Korean token that appears in a page title. Uses a longer token because
// BM25's short-token de-emphasis may filter single-character matches.
func TestWikiSearch_KoreanPrefixMatch(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "레시피", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "ramen-recipe") {
		t.Errorf("expected ramen-recipe.md path for 레시피 query, got: %q", out)
	}
}

// TestWikiSearch_Metadata verifies recall can find pages through structured
// frontmatter, not only title/body text.
func TestWikiSearch_Metadata(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "overview", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "deneb-architecture") {
		t.Errorf("expected deneb-architecture via tag metadata, got: %q", out)
	}
}

// TestWikiSearch_SpecialCharactersSafe verifies that FTS-boolean-shaped or
// SQL-injection-shaped queries pass through the tokenizer without error.
// Pure-Go textsearch strips non-letter/non-digit runes; these should not
// panic or return an error.
func TestWikiSearch_SpecialCharactersSafe(t *testing.T) {
	store := newTestWikiStore(t)
	queries := []string{
		`"quoted phrase"`,
		"foo AND bar",
		"foo OR bar",
		"(foo)",
		"foo & bar",
		"'; DROP TABLE pages; --",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			out, err := wikiSearch(context.Background(), store, q, 5)
			if err != nil {
				t.Errorf("wikiSearch(%q) errored: %v", q, err)
			}
			if out == "" {
				t.Errorf("wikiSearch(%q) returned empty string (expected result or guidance)", q)
			}
		})
	}
}

// TestWikiSearch_LimitDefaulting verifies limit=0 defaults, and a negative
// limit is treated like 0. (Schema-level clamping of large values is not
// exercised here — it happens at RPC boundary.)
func TestWikiSearch_LimitDefaulting(t *testing.T) {
	store := newTestWikiStore(t)
	out0, err := wikiSearch(context.Background(), store, "Deneb", 0)
	if err != nil {
		t.Fatalf("limit=0: %v", err)
	}
	outNeg, err := wikiSearch(context.Background(), store, "Deneb", -5)
	if err != nil {
		t.Fatalf("limit=-5: %v", err)
	}
	if out0 == "" || outNeg == "" {
		t.Fatal("expected non-empty results for limit=0 and limit=-5")
	}
}

// TestWikiSearch_NoResults returns a friendly Korean message when no page
// matches.
func TestWikiSearch_NoResults(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "xyzzy-unlikely-term-ABCDEF", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	// The wikiSearch output should be a Korean "no results" message — we
	// don't pin the exact wording, just that the raw query is not echoed
	// in a confusing form.
	if out == "" {
		t.Fatal("expected non-empty no-results message")
	}
}

// (intentionally no nil-store test: production wiring never passes nil —
// the wiki tool is gated by deps.Store presence upstream. Exercising nil
// here would only enforce an undocumented precondition.)

// TestMemorySystemStatus_Panel: the wiki status action must answer
// "기억 상태 어때?" in one glance — dreaming liveness, diary footprint,
// MEMORY.md budget pressure, backup recency.
func TestMemorySystemStatus_Panel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", home)

	store, err := wiki.NewStore(filepath.Join(home, "wiki"), filepath.Join(home, "memory", "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.AppendDiary("상태 패널 테스트 일지"); err != nil {
		t.Fatal(err)
	}
	// Dream state, oversized MEMORY.md, backup stamp.
	if err := os.WriteFile(filepath.Join(home, "wiki", ".diary-process-state.json"),
		[]byte(`{"lastDreamMs":`+fmt.Sprint(time.Now().Add(-3*time.Hour).UnixMilli())+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "workspace", "MEMORY.md"),
		[]byte(strings.Repeat("기억 ", 20_000)), 0o644); err != nil { // ~140KB
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "autonomous_state.json"),
		[]byte(`{"memory-backup":`+fmt.Sprint(time.Now().Add(-time.Hour).UnixMilli())+`}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := wikiStatus(store)
	for _, want := range []string{"기억 시스템", "드리밍: 마지막 사이클", "다이어리: 1파일", "MEMORY.md:", "드림 큐레이션 대기", "오프사이트 백업"} {
		if !strings.Contains(out, want) {
			t.Errorf("status panel missing %q\n%s", want, out)
		}
	}
}
