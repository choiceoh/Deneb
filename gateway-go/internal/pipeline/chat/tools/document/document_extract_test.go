package document

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCSVToMarkdownFormatsTable(t *testing.T) {
	csv := "이름,수량,단가\n모듈,100,5000\n인버터,20,30000\n"
	got, err := csvToMarkdown([]byte(csv))
	if err != nil {
		t.Fatalf("csvToMarkdown: %v", err)
	}
	for _, want := range []string{
		"| 이름 | 수량 | 단가 |",
		"| --- | --- | --- |",
		"| 모듈 | 100 | 5000 |",
		"| 인버터 | 20 | 30000 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestCSVToMarkdownFormatsRaggedRow(t *testing.T) {
	// A short row must not break the table; it pads to the header width.
	got, err := csvToMarkdown([]byte("a,b,c\n1\n"))
	if err != nil {
		t.Fatalf("csvToMarkdown: %v", err)
	}
	if !strings.Contains(got, "| 1 |  |  |") {
		t.Errorf("ragged row not padded:\n%s", got)
	}
}

func TestExtractDocumentTextReturnsXLSXByFilename(t *testing.T) {
	text, ok := ExtractDocumentText(context.Background(), makeTestXLSX(t), "report.xlsx", "")
	if !ok {
		t.Fatal("expected xlsx extraction to succeed")
	}
	if !strings.Contains(text, "| 품목 | 금액 |") {
		t.Errorf("xlsx not rendered as markdown table:\n%s", text)
	}
}

func TestExtractDocumentTextReturnsXLSXByMimeType(t *testing.T) {
	// No usable filename extension → must classify by MIME type.
	_, ok := ExtractDocumentText(context.Background(), makeTestXLSX(t), "download",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if !ok {
		t.Error("expected MIME-based xlsx detection to succeed")
	}
}

func TestExtractDocumentTextRejectsPlainText(t *testing.T) {
	if _, ok := ExtractDocumentText(context.Background(), []byte("hello"), "note.txt", "text/plain"); ok {
		t.Error("plain text is not a document — ExtractDocumentText should decline it")
	}
}

func TestExtractDocumentTextAllowsMarkdownOnly(t *testing.T) {
	md := []byte("# 분기 리뷰\n\n매출 1.2억, 마감 6/25.\n")
	text, ok := ExtractDocumentText(context.Background(), md, "review.md", "")
	if !ok {
		t.Fatal("expected markdown extraction to succeed")
	}
	if !strings.Contains(text, "매출 1.2억") {
		t.Errorf("markdown body not returned:\n%s", text)
	}
	// The same bytes named .txt must still be declined — only Markdown is promoted.
	if _, ok := ExtractDocumentText(context.Background(), md, "review.txt", ""); ok {
		t.Error("raw .txt should stay declined")
	}
}

func TestIsExtractableDocumentReturnsTrueForKnownTypes(t *testing.T) {
	yes := []struct{ mime, name string }{
		{"application/pdf", ""},
		{"", "a.xlsx"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", ""},
		{"application/vnd.oasis.opendocument.text", ""},
		{"text/csv", ""},
		{"", "data.csv"},
		{"", "readme.md"},
		{"text/markdown", ""},
	}
	for _, c := range yes {
		if !IsExtractableDocument(c.mime, c.name) {
			t.Errorf("IsExtractableDocument(%q, %q) = false, want true", c.mime, c.name)
		}
	}
	no := []struct{ mime, name string }{
		{"text/html", "page.html"},
		{"text/plain", "note.txt"},
		{"image/png", "a.png"},
		{"", ""},
	}
	for _, c := range no {
		if IsExtractableDocument(c.mime, c.name) {
			t.Errorf("IsExtractableDocument(%q, %q) = true, want false", c.mime, c.name)
		}
	}
}

func TestColumnGapsReturnsGapCount(t *testing.T) {
	cases := map[string]int{
		"품목       수량      단가": 2, // two multi-space gaps → 3 columns
		"모듈       100":        1,
		"단일 단어 단어들":           0, // single spaces are not column gaps
		"":                    0,
		"   ":                 0,
	}
	for line, want := range cases {
		if got := columnGaps(line); got != want {
			t.Errorf("columnGaps(%q) = %d, want %d", line, got, want)
		}
	}
}

func TestPageHasTableReturnsTrueForAlignedColumns(t *testing.T) {
	table := "견적서\n" +
		"품목       수량      단가\n" +
		"모듈       100       5000\n" +
		"인버터     20        30000\n"
	if !pageHasTable(table) {
		t.Error("aligned-column block should be detected as a table")
	}

	prose := "이것은 일반 문단입니다 표가 아니라 그냥 줄글이며\n" +
		"여러 줄에 걸쳐 있지만 컬럼 정렬이 전혀 없습니다\n" +
		"따라서 표로 감지되면 안 됩니다\n"
	if pageHasTable(prose) {
		t.Error("prose should not be detected as a table")
	}
}

func TestSplitPDFPagesKeepsEmptyEdgePages(t *testing.T) {
	// An image-only cover page yields no text, so raw pdftotext output starts
	// with a bare form feed. The empty page must survive the split — trimming
	// it would shift every page index off by one (the regression that made the
	// visual upgrade OCR the wrong pages on a real 소개자료 PDF).
	pages := splitPDFPages("\f2페이지 본문\f3페이지 본문\f")
	if want := []string{"", "2페이지 본문", "3페이지 본문"}; !reflect.DeepEqual(pages, want) {
		t.Errorf("leading empty page: splitPDFPages = %q, want %q", pages, want)
	}
	// A trailing image-only page (back cover) must survive too — only the
	// final page terminator is stripped.
	pages = splitPDFPages("1페이지 본문\f\f")
	if want := []string{"1페이지 본문", ""}; !reflect.DeepEqual(pages, want) {
		t.Errorf("trailing empty page: splitPDFPages = %q, want %q", pages, want)
	}
}

func TestClassifyPagesSeparatesTableAndVisualPages(t *testing.T) {
	prose := strings.Repeat("이 페이지는 표도 그림도 아닌 충분히 긴 일반 본문 문단입니다. ", 8)
	table := "품목       수량      단가\n" +
		"모듈       100       5000\n" +
		"인버터     20        30000\n"
	caption := "그림 3. 연도별 발전량 추이\n\n12"

	tableIdx, visualIdx := classifyPages([]string{prose, table, "", caption})
	if !reflect.DeepEqual(tableIdx, []int{1}) {
		t.Errorf("tableIdx = %v, want [1]", tableIdx)
	}
	// Page 2 (empty text layer) and page 3 (caption only) are visual candidates;
	// the prose page must qualify as neither.
	if !reflect.DeepEqual(visualIdx, []int{2, 3}) {
		t.Errorf("visualIdx = %v, want [2 3]", visualIdx)
	}
}

func TestPageNearlyEmptyThreshold(t *testing.T) {
	for _, sparse := range []string{"", "   ", "12", "그림 3. 연도별 발전량 추이   7"} {
		if !pageNearlyEmpty(sparse) {
			t.Errorf("pageNearlyEmpty(%q) = false, want true", sparse)
		}
	}
	long := strings.Repeat("실제 본문이 이어지는 문단, ", 20)
	if pageNearlyEmpty(long) {
		t.Errorf("%d-rune prose page classified as nearly empty", len([]rune(long)))
	}
}

func TestUpgradePageGuards(t *testing.T) {
	mdTable := "| 품목 | 수량 |\n| --- | --- |\n| 모듈 | 100 |"

	// Table page: only a confirmed markdown table replaces the original.
	if got, ok := upgradePage("orig", mdTable, true); !ok || got != mdTable {
		t.Errorf("table page with markdown OCR: got (%q, %v), want replacement", got, ok)
	}
	if _, ok := upgradePage("orig", "표가 아닌 그냥 줄글", true); ok {
		t.Error("table page must keep pdftotext when OCR produced no markdown table")
	}

	// Visual page: OCR must have recovered more than the text layer carried,
	// and the replacement is labeled as machine-read.
	got, ok := upgradePage("  12  ", "설비 전경 사진: 태양광 모듈 어레이와 인버터실 외관", false)
	if !ok || !strings.HasPrefix(got, "[그림/차트 페이지 OCR]\n") {
		t.Errorf("visual page recovery: got (%q, %v), want labeled replacement", got, ok)
	}
	if _, ok := upgradePage("원문 텍스트 레이어가 이미 더 길고 충분한 페이지", "짧음", false); ok {
		t.Error("visual page must keep pdftotext when OCR recovered less than the original")
	}
	if _, ok := upgradePage("", "   ", false); ok {
		t.Error("blank OCR output must not replace anything")
	}
}

// TestPDFStructuredExtractionLive runs the full structured-extraction chain
// (pdftotext → classify → selective raster → PaddleOCR-VL) against a real PDF
// and a live OCR server. Opt-in only:
//
//	DENEB_PDF_STRUCTURED_LIVE=/path/to.pdf go test -run PDFStructuredExtractionLive -v ./...
//
// Skipped in CI (no GPU / no poppler). Used to confirm the chain end-to-end on
// the DGX host — including visual-page recovery when the PDF has figure pages.
func TestPDFStructuredExtractionLive(t *testing.T) {
	path := os.Getenv("DENEB_PDF_STRUCTURED_LIVE")
	if path == "" {
		t.Skip("set DENEB_PDF_STRUCTURED_LIVE=/path/to.pdf to run against live CLIs + OCR server")
	}
	pdf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	text, err := pdfToTextStructured(context.Background(), pdf)
	if err != nil {
		t.Fatalf("pdfToTextStructured: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("empty extraction result")
	}
	t.Logf("extracted %d chars; visual pages recovered: %v\n%s",
		len(text), strings.Contains(text, "[그림/차트 페이지 OCR]"), text)
}
