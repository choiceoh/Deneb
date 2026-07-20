package web

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAcademicQueryIntentTriggersOnPapersOnly(t *testing.T) {
	positives := []string{
		"prompt cache invalidation arxiv",
		"2607.12161 조사해줘",
		"LLM agent 관련 논문 찾아줘",
		"retrieval augmentation paper survey",
		"학술 자료: reciprocal rank fusion",
	}
	for _, q := range positives {
		if !academicQueryIntent(q) {
			t.Errorf("intent missed for %q", q)
		}
	}
	negatives := []string{
		"대한전선 해저케이블 수주 현황",
		"coupang delivery status",
		"삼성전자 주가",
		"golang mutex deadlock 해결",
	}
	for _, q := range negatives {
		if academicQueryIntent(q) {
			t.Errorf("intent false positive for %q", q)
		}
	}
}

func TestAcademicAPIQueryStripsTriggersAndKorean(t *testing.T) {
	cleaned, ascii := academicAPIQuery("prompt cache 논문 arxiv 무효화 invalidation")
	if strings.Contains(cleaned, "논문") || strings.Contains(cleaned, "arxiv") {
		t.Errorf("trigger tokens not stripped: %q", cleaned)
	}
	if !strings.Contains(cleaned, "무효화") {
		t.Errorf("topic Korean dropped from cleaned: %q", cleaned)
	}
	if strings.Contains(ascii, "무효화") || !strings.Contains(ascii, "invalidation") {
		t.Errorf("ascii variant wrong: %q", ascii)
	}
}

func TestParseArxivFeedExtractsHits(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2607.12161v2</id>
    <title>Token Reduction
	  Considered   Harmful</title>
    <summary>Long
	abstract   body here.</summary>
    <published>2026-07-15T00:00:00Z</published>
    <author><name>Kim Researcher</name></author>
    <author><name>Lee Author</name></author>
    <author><name>Park Third</name></author>
  </entry>
</feed>`
	hits, err := parseArxivFeed([]byte(feed))
	if err != nil || len(hits) != 1 {
		t.Fatalf("parse: %v, hits=%d", err, len(hits))
	}
	h := hits[0]
	if h.ArxivID != "2607.12161" || h.Year != "2026" {
		t.Errorf("id=%q year=%q", h.ArxivID, h.Year)
	}
	if h.Title != "Token Reduction Considered Harmful" {
		t.Errorf("title whitespace not collapsed: %q", h.Title)
	}
	if h.URL != "https://arxiv.org/abs/2607.12161" {
		t.Errorf("url = %q", h.URL)
	}
	if len(h.Authors) != 3 {
		t.Errorf("authors = %v", h.Authors)
	}
}

func TestParseS2ResponseExtractsHits(t *testing.T) {
	body := `{"data":[{"title":"Memory Consolidation","abstract":"Study of memory.","year":2025,
	 "url":"https://www.semanticscholar.org/paper/abc","citationCount":42,
	 "externalIds":{"ArXiv":"2607.13591"},"authors":[{"name":"A One"},{"name":"B Two"}]}]}`
	hits, err := parseS2Response([]byte(body))
	if err != nil || len(hits) != 1 {
		t.Fatalf("parse: %v, hits=%d", err, len(hits))
	}
	h := hits[0]
	if h.ArxivID != "2607.13591" || h.Citations != 42 || h.Year != "2025" {
		t.Errorf("hit = %+v", h)
	}
}

// withMockAcademicSources swaps the paper searchers for a test and neutralizes
// the HN source (use withMockHN to mock it explicitly).
func withMockAcademicSources(t *testing.T, arxiv, s2 func(context.Context, string, int) ([]academicHit, error)) {
	t.Helper()
	origA, origS, origH := arxivSearchFn, semanticScholarSearchFn, hnSearchFn
	arxivSearchFn, semanticScholarSearchFn = arxiv, s2
	hnSearchFn = func(context.Context, string, int) ([]hnHit, error) { return nil, nil }
	t.Cleanup(func() {
		arxivSearchFn, semanticScholarSearchFn, hnSearchFn = origA, origS, origH
	})
}

// withMockHN swaps the HN searcher; apply after withMockAcademicSources.
func withMockHN(t *testing.T, fn func(context.Context, string, int) ([]hnHit, error)) {
	t.Helper()
	orig := hnSearchFn
	hnSearchFn = fn
	t.Cleanup(func() { hnSearchFn = orig })
}

func TestAcademicLaneMergesAndDedupesByArxivID(t *testing.T) {
	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) {
			return []academicHit{
				{Source: "arXiv", Title: "Paper A", ArxivID: "2607.00001", Year: "2026", Abstract: "Alpha abstract"},
			}, nil
		},
		func(context.Context, string, int) ([]academicHit, error) {
			return []academicHit{
				{Source: "S2", Title: "Paper A (dup)", ArxivID: "2607.00001", Citations: 5},
				{Source: "S2", Title: "Paper B", Citations: 9, Year: "2024", Authors: []string{"X", "Y", "Z"}},
			}, nil
		})

	block := academicLane(context.Background(), "any query")
	if !strings.Contains(block, "학술·기술 레인") {
		t.Fatalf("label missing:\n%s", block)
	}
	if !strings.Contains(block, "Paper A") || strings.Contains(block, "Paper A (dup)") {
		t.Errorf("dedupe by arXiv id failed:\n%s", block)
	}
	if !strings.Contains(block, "Paper B") || !strings.Contains(block, "인용 9") {
		t.Errorf("s2-only hit missing:\n%s", block)
	}
	if !strings.Contains(block, "X, Y 외") {
		t.Errorf("author rendering wrong:\n%s", block)
	}
	if !strings.Contains(block, "초록: Alpha abstract") {
		t.Errorf("abstract missing:\n%s", block)
	}
}

func TestAcademicLaneEmptyWhenBothSourcesFail(t *testing.T) {
	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) { return nil, errors.New("arxiv down") },
		func(context.Context, string, int) ([]academicHit, error) { return nil, errors.New("s2 429") })
	if block := academicLane(context.Background(), "q"); block != "" {
		t.Errorf("expected empty block, got:\n%s", block)
	}
}

func TestStartAcademicLaneGating(t *testing.T) {
	if ch := startAcademicLane(context.Background(), "삼성전자 주가", false); ch != nil {
		t.Error("lane must stay off for non-academic query")
	}
	if got := joinAcademicLane(nil); got != "" {
		t.Errorf("nil join = %q", got)
	}

	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) {
			return []academicHit{{Title: "Forced Paper", ArxivID: "2607.99999"}}, nil
		},
		func(context.Context, string, int) ([]academicHit, error) { return nil, nil })
	ch := startAcademicLane(context.Background(), "삼성전자 주가", true)
	if ch == nil {
		t.Fatal("forced lane must run")
	}
	if block := joinAcademicLane(ch); !strings.Contains(block, "Forced Paper") {
		t.Errorf("forced lane block:\n%s", block)
	}
}

func TestWebToolAppendsAcademicLaneToSearchOutput(t *testing.T) {
	// No provider keys → the search path lands on the DuckDuckGo seam.
	t.Setenv("SERPER_API_KEY", "")
	t.Setenv("BRAVE_API_KEY", "")
	origDDG := duckDuckGoSearchFn
	duckDuckGoSearchFn = func(context.Context, string) (string, error) {
		return "1. Regular result | https://example.com", nil
	}
	t.Cleanup(func() { duckDuckGoSearchFn = origDDG })
	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) {
			return []academicHit{{Title: "Lane Paper", ArxivID: "2607.42424"}}, nil
		},
		func(context.Context, string, int) ([]academicHit, error) { return nil, nil })

	tool := Tool(NewFetchCache(), NewLocalAIExtractor(), nil)
	out, err := tool(context.Background(), []byte(`{"query":"cache invalidation arxiv"}`))
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	if !strings.Contains(out, "Regular result") {
		t.Errorf("main search results missing:\n%s", out)
	}
	idx := strings.Index(out, "학술·기술 레인")
	if idx < 0 || idx < strings.Index(out, "Regular result") {
		t.Errorf("academic lane must follow main results:\n%s", out)
	}
	if !strings.Contains(out, "Lane Paper") || !strings.Contains(out, "arXiv:2607.42424") {
		t.Errorf("lane content missing:\n%s", out)
	}
}

func TestParseHNResponseExtractsHits(t *testing.T) {
	body := `{"hits":[
	 {"title":"Prompt caching  deep dive","url":"https://arxiv.org/abs/2607.12161v1","points":312,"num_comments":140,"objectID":"41000001","created_at":"2026-07-18T02:00:00Z"},
	 {"title":"","url":"https://x.example","points":5,"num_comments":0,"objectID":"41000002","created_at":"2026-01-01T00:00:00Z"},
	 {"title":"Ask HN: cache strategies?","points":42,"num_comments":33,"objectID":"41000003","created_at":"2025-11-02T00:00:00Z"}]}`
	hits, err := parseHNResponse([]byte(body))
	if err != nil || len(hits) != 2 {
		t.Fatalf("parse: %v hits=%d", err, len(hits))
	}
	h := hits[0]
	if h.Title != "Prompt caching deep dive" || h.ArxivID != "2607.12161" || h.Points != 312 || h.Year != "2026" {
		t.Errorf("hit = %+v", h)
	}
	if h.ItemURL != "https://news.ycombinator.com/item?id=41000001" {
		t.Errorf("item url = %q", h.ItemURL)
	}
	if hits[1].URL != "" || hits[1].ArxivID != "" {
		t.Errorf("ask-hn hit = %+v", hits[1])
	}
}

func TestAcademicLaneRendersHNBlockWithPaperAnnotation(t *testing.T) {
	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) {
			return []academicHit{{Title: "Paper A", ArxivID: "2607.00001", Year: "2026"}}, nil
		},
		func(context.Context, string, int) ([]academicHit, error) { return nil, nil })
	withMockHN(t, func(context.Context, string, int) ([]hnHit, error) {
		return []hnHit{
			{Title: "Discussion of Paper A", ArxivID: "2607.00001", Points: 300, Comments: 120, ItemURL: "https://news.ycombinator.com/item?id=1", URL: "https://arxiv.org/abs/2607.00001"},
			{Title: "Unrelated tool thread", Points: 90, Comments: 40, Year: "2025", ItemURL: "https://news.ycombinator.com/item?id=2", URL: "https://blog.example/post"},
		}, nil
	})

	block := academicLane(context.Background(), "q")
	if !strings.Contains(block, "HN 토론") {
		t.Fatalf("hn block missing:\n%s", block)
	}
	if !strings.Contains(block, "Discussion of Paper A — 300p·120c [위 논문 토론]") {
		t.Errorf("paper annotation missing:\n%s", block)
	}
	if !strings.Contains(block, "Unrelated tool thread — 90p·40c (2025)") {
		t.Errorf("plain hn hit wrong:\n%s", block)
	}
	if !strings.Contains(block, "기사: https://blog.example/post") {
		t.Errorf("article link missing:\n%s", block)
	}
	// Papers render before the HN sub-block.
	if strings.Index(block, "Paper A") > strings.Index(block, "HN 토론") {
		t.Errorf("ordering wrong:\n%s", block)
	}
}

func TestAcademicLaneHNOnlyStillRenders(t *testing.T) {
	withMockAcademicSources(t,
		func(context.Context, string, int) ([]academicHit, error) { return nil, nil },
		func(context.Context, string, int) ([]academicHit, error) { return nil, nil })
	withMockHN(t, func(context.Context, string, int) ([]hnHit, error) {
		return []hnHit{{Title: "Solo thread", Points: 10, Comments: 3, ItemURL: "https://news.ycombinator.com/item?id=9"}}, nil
	})
	block := academicLane(context.Background(), "q")
	if !strings.Contains(block, "Solo thread") {
		t.Errorf("hn-only lane empty:\n%s", block)
	}
}
