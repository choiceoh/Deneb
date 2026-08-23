package wikitool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// TestNormalizeSourceURL locks the idempotency key: tracking params stripped,
// query sorted, fragment dropped, YouTube variants collapse to one watch URL.
func TestNormalizeSourceURL(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/a?utm_source=x&b=2&a=1#frag":                     "https://example.com/a?a=1&b=2",
		"https://example.com/a?fbclid=abc":                                    "https://example.com/a",
		"http://example.com/path":                                             "http://example.com/path",
		"https://youtu.be/dQw4w9WgXcQ?t=42":                                   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL1&utm_campaign=z": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                          "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":                           "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	for in, want := range cases {
		got, err := normalizeSourceURL(in)
		if err != nil {
			t.Errorf("normalizeSourceURL(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeSourceURL(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "ftp://x/y", "not a url at all ://"} {
		if _, err := normalizeSourceURL(bad); err == nil {
			t.Errorf("normalizeSourceURL(%q) must reject", bad)
		}
	}
}

// TestMaterialFilenameReturnsStableSlug: same URL → same name regardless of
// title drift's hash part; Korean titles survive slugification.
func TestMaterialFilenameReturnsStableSlug(t *testing.T) {
	a := materialFilename("발표 자료: Research OS!", "https://example.com/a")
	b := materialFilename("발표 자료: Research OS!", "https://example.com/a")
	if a != b {
		t.Errorf("filename not deterministic: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "발표-자료-research-os-") || !strings.HasSuffix(a, ".md") {
		t.Errorf("unexpected slug form: %q", a)
	}
	c := materialFilename("발표 자료: Research OS!", "https://example.com/b")
	if a == c {
		t.Error("different URLs must hash to different filenames")
	}
	if got := materialFilename("!!!", "https://example.com/x"); !strings.HasPrefix(got, "자료-") {
		t.Errorf("unsluggable title must fall back to 자료-, got %q", got)
	}
}

// TestFetchWebTextParsesHTMLAndRejectsBinary exercises the stdlib HTML
// stripper: title, script/style removal, entity decoding, and the non-HTML
// content-type rejection.
// withPlainIngestClient swaps the SSRF-safe ingest client for a plain one for
// the test's duration: httptest binds to 127.0.0.1, which the production SSRF
// dialer correctly rejects (that rejection is asserted in
// TestIngestHTTPClientRejectsLoopback).
func withPlainIngestClient(t *testing.T) {
	t.Helper()
	orig := ingestHTTPClient
	ingestHTTPClient = func() *http.Client { return &http.Client{Timeout: ingestFetchTimeout} }
	t.Cleanup(func() { ingestHTTPClient = orig })
}

// TestIngestHTTPClientRejectsLoopback pins the SSRF guard: the production
// ingest client must refuse to fetch a loopback address, so a prompt-injected
// page cannot make the gateway ingest internal endpoints.
func TestIngestHTTPClientRejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret internal data"))
	}))
	defer srv.Close()
	_, _, err := fetchWebText(context.Background(), srv.URL) // 127.0.0.1
	if err == nil {
		t.Fatal("ingest fetched a loopback URL — SSRF guard not active")
	}
	if !strings.Contains(err.Error(), "SSRF") && !strings.Contains(err.Error(), "private") {
		t.Errorf("expected SSRF/private rejection, got: %v", err)
	}
}

func TestFetchWebTextParsesHTMLAndRejectsBinary(t *testing.T) {
	withPlainIngestClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>테스트 &amp; 문서</title>
				<style>body{color:red}</style><script>var hidden="MUSTNOTAPPEAR"</script></head>
				<body><h1>제목</h1><p>본문 &quot;내용&quot; 입니다.</p></body></html>`))
		case "/binary":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4"))
		}
	}))
	defer srv.Close()

	title, text, err := fetchWebText(context.Background(), srv.URL+"/page")
	if err != nil {
		t.Fatalf("fetchWebText: %v", err)
	}
	if title != "테스트 & 문서" {
		t.Errorf("title = %q", title)
	}
	if strings.Contains(text, "MUSTNOTAPPEAR") || strings.Contains(text, "color:red") {
		t.Errorf("script/style leaked into text: %q", text)
	}
	if !strings.Contains(text, `본문 "내용" 입니다.`) {
		t.Errorf("entities not decoded or body missing: %q", text)
	}

	if _, _, err := fetchWebText(context.Background(), srv.URL+"/binary"); err == nil {
		t.Error("non-text content type must be rejected")
	}
}

// TestWikiIngestCreatesDedupsAndRoutesProject runs the full flow against a
// temp store and a local HTTP server, with the summarizer stubbed: page
// creation, idempotent dedup, project routing (existing vs unknown project),
// and the 로그 op-prefix append.
func TestWikiIngestCreatesDedupsAndRoutesProject(t *testing.T) {
	withPlainIngestClient(t)
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>계통연계 기준 개정 안내</title></head>
			<body><p>2026년 태양광 계통연계 기준이 개정되어 접속 용량 상한이 상향되었다.</p></body></html>`))
	}))
	defer srv.Close()

	origSummarize := ingestSummarize
	t.Cleanup(func() { ingestSummarize = origSummarize })
	ingestSummarize = func(_ context.Context, _, _ string, _ int, _ ...json.RawMessage) (string, error) {
		return "계통연계 기준 개정으로 접속 용량 상한 상향\n- 상한 상향\n적용: 인허가 검토에 반영", nil
	}

	// Existing project so the linked path is exercised.
	rep := &wiki.Page{Meta: wiki.Frontmatter{Title: "영산고", Category: "프로젝트"}, Body: "## 현재 상태\n"}
	if err := store.WritePage("프로젝트/영산고/대표.md", rep); err != nil {
		t.Fatalf("seed rep page: %v", err)
	}

	out, err := wikiIngest(context.Background(), store, srv.URL+"/doc?utm_source=share", "영산고", "", "인허가 참고", false)
	if err != nil {
		t.Fatalf("wikiIngest: %v", err)
	}
	if !strings.Contains(out, "자료 인제스트 완료") || !strings.Contains(out, "프로젝트/영산고/자료/") {
		t.Fatalf("unexpected result: %q", out)
	}

	// Page landed with provenance + summary + note.
	pathStart := strings.Index(out, "프로젝트/")
	pagePath := strings.Fields(out[pathStart:])[0]
	page, err := store.ReadPage(pagePath)
	if err != nil {
		t.Fatalf("read ingested page: %v", err)
	}
	normalized := srv.URL + "/doc" // tracking param stripped
	if page.Meta.Resource != normalized {
		t.Errorf("Resource = %q, want %q", page.Meta.Resource, normalized)
	}
	for _, want := range []string{"## 요약", "접속 용량", "- 메모: 인허가 참고", "## 원문 발췌", "개정되어"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("page body missing %q", want)
		}
	}

	// 로그 op-prefix append.
	log, err := store.ReadPage(wiki.LogPagePath("영산고"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(log.Body, "] ingest | 계통연계 기준 개정 안내") || !strings.Contains(log.Body, "[["+pagePath+"]]") {
		t.Errorf("log missing op-prefixed ingest section: %q", log.Body)
	}

	// Idempotent: same URL (different tracking params) dedups.
	out2, err := wikiIngest(context.Background(), store, srv.URL+"/doc?fbclid=zzz", "영산고", "", "", false)
	if err != nil {
		t.Fatalf("wikiIngest dedup: %v", err)
	}
	if !strings.Contains(out2, "이미 인제스트된 자료") || !strings.Contains(out2, pagePath) {
		t.Errorf("dedup miss: %q", out2)
	}

	// Unknown project routes to the global bucket with a notice.
	out3, err := wikiIngest(context.Background(), store, srv.URL+"/doc2", "없는프로젝트", "", "", false)
	if err != nil {
		t.Fatalf("wikiIngest unknown project: %v", err)
	}
	if !strings.Contains(out3, "프로젝트/자료/") || !strings.Contains(out3, "대표페이지가 없어") {
		t.Errorf("unknown project must fall back to global bucket with notice: %q", out3)
	}
}

func TestFindIngestedPageIgnoresSyntheticFactCrowding(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const normalized = "https://example.com/docs/alpha"
	materialPath := "프로젝트/자료/alpha-source.md"
	material := wiki.NewPage("Alpha source", "프로젝트", nil)
	material.Meta.Resource = normalized
	material.Body = "source: " + normalized
	if err := store.WritePage(materialPath, material); err != nil {
		t.Fatal(err)
	}
	decoy := wiki.NewPage("Alpha URL index", "업무", nil)
	decoy.Body = strings.Repeat(normalized+" ", 40)
	if err := store.WritePage("업무/alpha-url-index.md", decoy); err != nil {
		t.Fatal(err)
	}
	for index := range 12 {
		if _, err := store.UpsertFact(wiki.FactInput{
			Subject: "https example com docs alpha", Key: "field." + string(rune('a'+index)), Value: "value-" + string(rune('a'+index)),
			Kind: wiki.FactKindGeneric, Authority: wiki.FactAuthorityAgent,
		}); err != nil {
			t.Fatal(err)
		}
	}
	allPlane, err := store.Search(context.Background(), normalized, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range allPlane {
		if result.Path == materialPath {
			t.Fatalf("fixture did not crowd the material page on the all-plane search: %+v", allPlane)
		}
	}

	if got := findIngestedPage(context.Background(), store, normalized); got != materialPath {
		t.Fatalf("findIngestedPage() = %q, want %q", got, materialPath)
	}
}

// TestWikiIngest_SummaryFailOpen: an LLM outage must not lose the capture —
// the page lands with the excerpt fallback.
// TestWikiIngestLinksProjectWhenRepIsLegacyFlat pins the Codex-review fix: a
// project whose rep page is still the legacy flat form (프로젝트/<name>.md)
// must link the ingest into the project (folder 자료 slot + 로그 op + related
// to the flat rep), not silently fall back to the global bucket.
func TestWikiIngestLinksProjectWhenRepIsLegacyFlat(t *testing.T) {
	withPlainIngestClient(t)
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>모듈 단가 동향</title></head><body><p>단가 하락세.</p></body></html>`))
	}))
	defer srv.Close()

	origSummarize := ingestSummarize
	t.Cleanup(func() { ingestSummarize = origSummarize })
	ingestSummarize = func(_ context.Context, _, _ string, _ int, _ ...json.RawMessage) (string, error) {
		return "모듈 단가 하락세\n- 하락\n적용: 발주 시점 판단", nil
	}

	legacyRep := "프로젝트/구프로젝트.md"
	rep := &wiki.Page{Meta: wiki.Frontmatter{Title: "구프로젝트", Category: "프로젝트"}, Body: "## 현재 상태\n"}
	if err := store.WritePage(legacyRep, rep); err != nil {
		t.Fatalf("seed legacy rep: %v", err)
	}

	out, err := wikiIngest(context.Background(), store, srv.URL, "구프로젝트", "", "", false)
	if err != nil {
		t.Fatalf("wikiIngest: %v", err)
	}
	if strings.Contains(out, "전역 자료 버킷") {
		t.Fatalf("legacy flat rep fell back to global bucket: %q", out)
	}
	if !strings.Contains(out, "프로젝트/구프로젝트/자료/") {
		t.Fatalf("material not filed under the project: %q", out)
	}
	if !strings.Contains(out, "로그: 프로젝트/구프로젝트/로그.md") {
		t.Fatalf("ingest log not appended for legacy project: %q", out)
	}

	// Related must point at the rep form that exists (the flat page), not a
	// dead folder path.
	matPath := ""
	pages, _ := store.ListPages("프로젝트")
	for _, p := range pages {
		if strings.HasPrefix(p, "프로젝트/구프로젝트/자료/") {
			matPath = p
		}
	}
	if matPath == "" {
		t.Fatal("material page not found")
	}
	mat, err := store.ReadPage(matPath)
	if err != nil {
		t.Fatalf("read material: %v", err)
	}
	if len(mat.Meta.Related) != 1 || mat.Meta.Related[0] != legacyRep {
		t.Errorf("related=%v, want [%s]", mat.Meta.Related, legacyRep)
	}
}

func TestWikiIngest_SummaryFailOpen(t *testing.T) {
	withPlainIngestClient(t)
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>t</title></head><body>실패해도 남는 본문</body></html>`))
	}))
	defer srv.Close()

	origSummarize := ingestSummarize
	t.Cleanup(func() { ingestSummarize = origSummarize })
	ingestSummarize = func(_ context.Context, _, _ string, _ int, _ ...json.RawMessage) (string, error) {
		return "", context.DeadlineExceeded
	}

	out, err := wikiIngest(context.Background(), store, srv.URL, "", "", "", false)
	if err != nil {
		t.Fatalf("wikiIngest: %v", err)
	}
	if !strings.Contains(out, "자료 인제스트 완료") {
		t.Fatalf("capture lost on summarizer failure: %q", out)
	}
	pagePath := strings.Fields(out[strings.Index(out, "프로젝트/"):])[0]
	page, err := store.ReadPage(pagePath)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if !strings.Contains(page.Body, "자동 요약 실패") || !strings.Contains(page.Body, "실패해도 남는 본문") {
		t.Errorf("fail-open excerpt missing: %q", page.Body)
	}
}
