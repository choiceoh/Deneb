package tools

import (
	"context"
	"strings"
	"testing"
)

func TestCSVToMarkdown(t *testing.T) {
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

func TestCSVToMarkdown_Ragged(t *testing.T) {
	// A short row must not break the table; it pads to the header width.
	got, err := csvToMarkdown([]byte("a,b,c\n1\n"))
	if err != nil {
		t.Fatalf("csvToMarkdown: %v", err)
	}
	if !strings.Contains(got, "| 1 |  |  |") {
		t.Errorf("ragged row not padded:\n%s", got)
	}
}

func TestExtractDocumentText_XLSXByName(t *testing.T) {
	text, ok := ExtractDocumentText(context.Background(), makeTestXLSX(t), "report.xlsx", "")
	if !ok {
		t.Fatal("expected xlsx extraction to succeed")
	}
	if !strings.Contains(text, "| 품목 | 금액 |") {
		t.Errorf("xlsx not rendered as markdown table:\n%s", text)
	}
}

func TestExtractDocumentText_XLSXByMime(t *testing.T) {
	// No usable filename extension → must classify by MIME type.
	_, ok := ExtractDocumentText(context.Background(), makeTestXLSX(t), "download",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if !ok {
		t.Error("expected MIME-based xlsx detection to succeed")
	}
}

func TestExtractDocumentText_Unsupported(t *testing.T) {
	if _, ok := ExtractDocumentText(context.Background(), []byte("hello"), "note.txt", "text/plain"); ok {
		t.Error("plain text is not a document — ExtractDocumentText should decline it")
	}
}

func TestExtractDocumentText_Markdown(t *testing.T) {
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

func TestIsExtractableDocument(t *testing.T) {
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

func TestColumnGaps(t *testing.T) {
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

func TestPageHasTable(t *testing.T) {
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
