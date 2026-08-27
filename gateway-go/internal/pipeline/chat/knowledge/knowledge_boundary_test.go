package knowledge

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func newKnowledgeBoundaryStore(t *testing.T) *wiki.Store {
	t.Helper()
	root := t.TempDir()
	store, err := wiki.NewStore(root+"/wiki", root+"/diary")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func writeKnowledgeBoundaryPage(t *testing.T, store *wiki.Store, path, title, summary, body string, importance float64, archived bool) {
	t.Helper()
	page := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID:         strings.TrimSuffix(strings.ReplaceAll(path, "/", "-"), ".md"),
			Title:      title,
			Summary:    summary,
			Category:   "기타",
			Importance: importance,
			Archived:   archived,
		},
		Body: body,
	}
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("WritePage(%q): %v", path, err)
	}
}

func TestBoundaryFormatTier1EmptyStoreAndThresholdMatrix(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	tests := []struct {
		name      string
		threshold float64
	}{
		{name: "negative threshold with no pages", threshold: -1},
		{name: "zero threshold with no pages", threshold: 0},
		{name: "ordinary threshold with no pages", threshold: 0.8},
		{name: "one threshold with no pages", threshold: 1},
		{name: "above one threshold with no pages", threshold: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTier1(store, tt.threshold); got != "" {
				t.Fatalf("FormatTier1(empty, %v) = %q", tt.threshold, got)
			}
		})
	}
}

func TestBoundaryFormatTier1ThresholdIsInclusive(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	writeKnowledgeBoundaryPage(t, store, "기타/below.md", "Below", "", "below body", 0.79, false)
	writeKnowledgeBoundaryPage(t, store, "기타/exact.md", "Exact", "", "exact body", 0.8, false)
	writeKnowledgeBoundaryPage(t, store, "기타/above.md", "Above", "", "above body", 0.9, false)

	got := FormatTier1(store, 0.8)
	if strings.Contains(got, "Below") || strings.Contains(got, "below body") {
		t.Fatalf("below-threshold page included:\n%s", got)
	}
	for _, want := range []string{"Above", "above body", "Exact", "exact body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("inclusive threshold output missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "Above") > strings.Index(got, "Exact") {
		t.Fatalf("importance order is not descending:\n%s", got)
	}
}

func TestBoundaryFormatTier1SkipsArchivedPages(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	writeKnowledgeBoundaryPage(t, store, "기타/active.md", "Active", "current", "active body", 0.9, false)
	writeKnowledgeBoundaryPage(t, store, "기타/archived.md", "Archived", "old", "archived body", 1.0, true)
	got := FormatTier1(store, 0)
	if strings.Contains(got, "Archived") || strings.Contains(got, "archived body") {
		t.Fatalf("archived page leaked into tier 1:\n%s", got)
	}
	for _, want := range []string{"Active", "current", "active body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("active output missing %q:\n%s", want, got)
		}
	}
}

func TestBoundaryFormatTier1SectionGrammarWithSummary(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	writeKnowledgeBoundaryPage(t, store, "업무/계약.md", "계약 기준", "계약 검토 기준 요약", "본문 첫 줄\n본문 둘째 줄", 0.95, false)
	want := "## 핵심 지식 (자동 주입)\n\n" +
		"### 계약 기준 (업무/계약.md)\n" +
		"_계약 검토 기준 요약_\n" +
		"본문 첫 줄\n본문 둘째 줄\n\n"
	if got := FormatTier1(store, 0.9); got != want {
		t.Fatalf("FormatTier1() =\n%q\nwant\n%q", got, want)
	}
}

func TestBoundaryFormatTier1SectionGrammarWithoutSummary(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	writeKnowledgeBoundaryPage(t, store, "사용자/선호.md", "사용자 선호", "", "결론부터 보고한다.", 1, false)
	want := "## 핵심 지식 (자동 주입)\n\n" +
		"### 사용자 선호 (사용자/선호.md)\n" +
		"결론부터 보고한다.\n\n"
	if got := FormatTier1(store, 1); got != want {
		t.Fatalf("FormatTier1() =\n%q\nwant\n%q", got, want)
	}
}

func TestBoundaryFormatTier1PreservesMarkdownAndUnicode(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	body := "- **결정**: 진행\n- 담당: 선택님 📎\n\n```json\n{\"ok\":true}\n```"
	writeKnowledgeBoundaryPage(t, store, "프로젝트/유니코드.md", "태양광 프로젝트", "핵심 일정 📅", body, 1, false)
	got := FormatTier1(store, 0)
	for _, want := range []string{
		"태양광 프로젝트",
		"_핵심 일정 📅_",
		"- **결정**: 진행",
		"담당: 선택님 📎",
		"```json",
		`{"ok":true}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if !utf8.ValidString(got) {
		t.Fatalf("output is invalid UTF-8: %x", []byte(got))
	}
}

func TestBoundaryFormatTier1TruncatesBodyByRunes(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	body := strings.Repeat("가", tier1MaxBodyRunes+50)
	writeKnowledgeBoundaryPage(t, store, "기타/long.md", "Long", "", body, 1, false)
	got := FormatTier1(store, 0)
	if !strings.Contains(got, strings.Repeat("가", tier1MaxBodyRunes)+"...") {
		t.Fatal("long Unicode body was not truncated at the rune boundary")
	}
	if strings.Contains(got, strings.Repeat("가", tier1MaxBodyRunes+1)) {
		t.Fatal("body exceeded per-page rune cap")
	}
	if !utf8.ValidString(got) {
		t.Fatal("body truncation split UTF-8")
	}
}

func TestBoundaryFormatTier1DoesNotTruncateExactBodyLimit(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	body := strings.Repeat("x", tier1MaxBodyRunes)
	writeKnowledgeBoundaryPage(t, store, "기타/exact.md", "Exact", "", body, 1, false)
	got := FormatTier1(store, 0)
	if !strings.Contains(got, body+"\n\n") {
		t.Fatal("exact-limit body not preserved")
	}
	if strings.Contains(got, body+"...") {
		t.Fatal("exact-limit body was unnecessarily truncated")
	}
}

func TestBoundaryFormatTier1CapsPageCount(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	for i := 0; i < tier1MaxPages+5; i++ {
		importance := 1.0 - float64(i)/100
		writeKnowledgeBoundaryPage(
			t,
			store,
			fmt.Sprintf("기타/page-%02d.md", i),
			fmt.Sprintf("Page %02d", i),
			"",
			fmt.Sprintf("body-%02d", i),
			importance,
			false,
		)
	}
	got := FormatTier1(store, 0)
	if count := strings.Count(got, "### Page "); count != tier1MaxPages {
		t.Fatalf("rendered pages = %d, want %d\n%s", count, tier1MaxPages, got)
	}
	for i := 0; i < tier1MaxPages; i++ {
		if !strings.Contains(got, fmt.Sprintf("Page %02d", i)) {
			t.Fatalf("top page %02d missing", i)
		}
	}
	for i := tier1MaxPages; i < tier1MaxPages+5; i++ {
		if strings.Contains(got, fmt.Sprintf("Page %02d", i)) {
			t.Fatalf("page beyond cap %02d included", i)
		}
	}
}

func TestBoundaryFormatTier1DescendingImportanceOrder(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	pages := []struct {
		path       string
		title      string
		importance float64
	}{
		{path: "기타/low.md", title: "Low", importance: 0.1},
		{path: "기타/high.md", title: "High", importance: 1.0},
		{path: "기타/mid-high.md", title: "Mid High", importance: 0.75},
		{path: "기타/mid-low.md", title: "Mid Low", importance: 0.25},
		{path: "기타/middle.md", title: "Middle", importance: 0.5},
	}
	for _, page := range pages {
		writeKnowledgeBoundaryPage(t, store, page.path, page.title, "", page.title+" body", page.importance, false)
	}
	got := FormatTier1(store, 0)
	wantOrder := []string{"### High ", "### Mid High ", "### Middle ", "### Mid Low ", "### Low "}
	last := -1
	for _, marker := range wantOrder {
		index := strings.Index(got, marker)
		if index < 0 {
			t.Fatalf("missing marker %q:\n%s", marker, got)
		}
		if index <= last {
			t.Fatalf("marker %q out of order:\n%s", marker, got)
		}
		last = index
	}
}

func TestBoundaryFormatTier1TotalBudgetStopsBeforeOverflow(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	// Per-page body truncation makes each entry bounded. Long summaries and
	// titles fill the remaining section budget so the total guard is exercised.
	for i := 0; i < tier1MaxPages; i++ {
		writeKnowledgeBoundaryPage(
			t,
			store,
			fmt.Sprintf("기타/budget-%02d.md", i),
			fmt.Sprintf("%02d-%s", i, strings.Repeat("T", 1050)),
			strings.Repeat("S", 1050),
			strings.Repeat("B", tier1MaxBodyRunes),
			1.0-float64(i)/100,
			false,
		)
	}
	got := FormatTier1(store, 0)
	if len(got) > tier1MaxTotalChar {
		t.Fatalf("output bytes = %d, budget = %d", len(got), tier1MaxTotalChar)
	}
	if !strings.Contains(got, "00-") {
		t.Fatal("budget guard omitted first entry")
	}
	if strings.Count(got, "### ") >= tier1MaxPages {
		t.Fatalf("fixture failed to exercise total budget:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatal("budgeted output ended with a partial entry")
	}
}

func TestBoundaryFormatTier1TokenBudgetStopsBeforeOverflow(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	for i := 0; i < tier1MaxPages; i++ {
		writeKnowledgeBoundaryPage(
			t,
			store,
			fmt.Sprintf("기타/token-budget-%02d.md", i),
			fmt.Sprintf("토큰 예산 페이지 %02d", i),
			strings.Repeat("요약", 120),
			strings.Repeat("본문", tier1MaxBodyRunes),
			1.0-float64(i)/100,
			false,
		)
	}
	got := FormatTier1(store, 0)
	if tokens := tokenest.Estimate(got); tokens > tier1MaxTokens {
		t.Fatalf("output tokens = %d, budget = %d", tokens, tier1MaxTokens)
	}
	if !strings.Contains(got, "토큰 예산 페이지 00") {
		t.Fatal("token budget guard omitted first entry")
	}
	if strings.Count(got, "### ") >= tier1MaxPages {
		t.Fatalf("fixture failed to exercise token budget:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatal("budgeted output ended with a partial entry")
	}
}

func TestBoundaryFormatTier1FirstOversizedEntryYieldsHeaderOnly(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	writeKnowledgeBoundaryPage(
		t,
		store,
		"기타/oversized.md",
		strings.Repeat("T", tier1MaxTotalChar),
		"summary",
		"body",
		1,
		false,
	)
	got := FormatTier1(store, 0)
	want := "## 핵심 지식 (자동 주입)\n\n"
	if got != want {
		t.Fatalf("oversized first entry produced partial content: length=%d prefix=%q", len(got), got[:min(len(got), 80)])
	}
}

func TestBoundaryFormatTier1ConcurrentReadsAreStable(t *testing.T) {
	store := newKnowledgeBoundaryStore(t)
	for i := 0; i < 8; i++ {
		writeKnowledgeBoundaryPage(
			t,
			store,
			fmt.Sprintf("기타/concurrent-%d.md", i),
			fmt.Sprintf("Concurrent %d", i),
			"summary",
			fmt.Sprintf("body %d", i),
			1-float64(i)/10,
			false,
		)
	}
	want := FormatTier1(store, 0)
	const workers = 64
	start := make(chan struct{})
	results := make(chan string, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			results <- FormatTier1(store, 0)
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		if got := <-results; got != want {
			t.Fatalf("worker %d output drifted:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

func TestBoundaryTier1ConstantsRemainProtective(t *testing.T) {
	if tier1MaxPages != 10 {
		t.Fatalf("tier1MaxPages = %d", tier1MaxPages)
	}
	if tier1MaxBodyRunes != 1000 {
		t.Fatalf("tier1MaxBodyRunes = %d", tier1MaxBodyRunes)
	}
	if tier1MaxTotalChar != 20000 {
		t.Fatalf("tier1MaxTotalChar = %d", tier1MaxTotalChar)
	}
	if tier1MaxTokens != 8000 {
		t.Fatalf("tier1MaxTokens = %d", tier1MaxTokens)
	}
	if tier1MaxBodyRunes*tier1MaxPages >= tier1MaxTotalChar {
		t.Fatalf("page bodies alone can exhaust total budget: %d >= %d", tier1MaxBodyRunes*tier1MaxPages, tier1MaxTotalChar)
	}
}
