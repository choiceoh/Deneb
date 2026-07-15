package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQueryPlanTypesClausesIntentAndScope(t *testing.T) {
	plan := ParseQueryPlan("lex: 계약 일정\nvec: payment milestone\nhyde: 선수금이 다음 주 입금된다\nintent: 대한전선 계약\nscope: 프로젝트/대한전선")
	if len(plan.Clauses) != 3 || plan.Clauses[0].Kind != QueryKindLex || plan.Clauses[1].Kind != QueryKindVec || plan.Clauses[2].Kind != QueryKindHyDE {
		t.Fatalf("clauses = %#v", plan.Clauses)
	}
	if plan.Intent != "대한전선 계약" || len(plan.Scopes) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestParseQueryPlanPreservesPlainTextAlongsideOperators(t *testing.T) {
	plan := ParseQueryPlan("lex: 계약 일정\n이번 주 입금\nscope: 프로젝트")
	if len(plan.Clauses) != 3 || plan.Clauses[1].Kind != QueryKindLex || plan.Clauses[2].Kind != QueryKindVec {
		t.Fatalf("clauses = %#v", plan.Clauses)
	}
	if plan.Clauses[1].Query != "이번 주 입금" || len(plan.Scopes) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPageBodyStartLineIgnoresMatchingFrontmatterText(t *testing.T) {
	page := &Page{Meta: Frontmatter{Title: "same text", Summary: "same text"}, Body: "same text\nnext"}
	rendered := string(page.Render())
	want := strings.Count(rendered[:len(rendered)-len(page.Body)], "\n") + 1
	if got := pageBodyStartLine(page); got != want {
		t.Fatalf("body start = %d, want %d", got, want)
	}
}

func TestSearchEmptyBodyReturnsVisibleIdentity(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mustWrite(t, store, "시스템/empty.md", &Page{Meta: Frontmatter{Title: "검색 설정", Summary: "운영 문서", Cues: []string{"벡터 상태 점검"}}})
	results, err := store.Search(context.Background(), "벡터 상태 점검", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Content != "검색 설정 — 운영 문서" || results[0].Line != 1 {
		t.Fatalf("results = %+v", results)
	}
}

func TestValidCachedSemanticPageRejectsInconsistentDimensions(t *testing.T) {
	cached := cachedVec{vec: []float32{1, 2}, chunks: []semanticChunk{{vec: []float32{1, 2}}, {vec: []float32{1}}}}
	if validCachedSemanticPage(cached, 2) {
		t.Fatal("mixed chunk dimensions should be stale")
	}
}

func TestSearchPlanAppliesScopeBeforeLimitAndReturnsHierarchy(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mustWrite(t, store, "프로젝트/대한전선/대표.md", &Page{Meta: Frontmatter{Title: "대한전선 계약", Category: "프로젝트", Client: "대한전선"}, Body: "계약 일정은 8월이다."})
	mustWrite(t, store, "업무/계약.md", &Page{Meta: Frontmatter{Title: "일반 계약", Category: "업무"}, Body: "계약 일정 템플릿"})
	report, err := store.SearchPlan(context.Background(), QueryPlan{
		Clauses: []QueryClause{{Kind: QueryKindLex, Query: "계약 일정"}},
		Scopes:  []string{"프로젝트/대한전선"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || report.Results[0].Path != "프로젝트/대한전선/대표.md" {
		t.Fatalf("results = %#v", report.Results)
	}
	if report.Results[0].Line <= 0 || report.Results[0].EndLine < report.Results[0].Line || len(report.Results[0].Context) < 2 {
		t.Fatalf("address/context = %#v", report.Results[0])
	}
}

func TestSearchPlanBatchesSemanticClauses(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mustWrite(t, store, "프로젝트/risk.md", &Page{Meta: Frontmatter{Title: "위험"}, Body: "납기 차질 우려"})
	embedder := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true}}
	store.SetEmbedder(embedder)
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	embedder.calls = 0
	_, err = store.SearchPlan(context.Background(), QueryPlan{Clauses: []QueryClause{
		{Kind: QueryKindVec, Query: "납기 지연 위험"},
		{Kind: QueryKindHyDE, Query: "공급 일정에 차질 우려가 있다"},
	}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if embedder.calls != 1 {
		t.Fatalf("semantic query Embed calls = %d, want one batch", embedder.calls)
	}
}

func TestSearchDoctorProbesSemanticAndReranker(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mustWrite(t, store, "시스템/search.md", &Page{Meta: Frontmatter{Title: "검색", Category: "시스템"}, Body: "검색 상태 점검"})
	store.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "fake:2", dimensions: 2})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.SetReranker(&fakeReranker{})
	report := store.SearchDoctor(context.Background())
	if !report.Healthy || !report.SemanticProbe.Healthy || report.SemanticProbe.Dimensions != 2 || !report.Reranker.Healthy {
		t.Fatalf("doctor = %+v", report)
	}
}
