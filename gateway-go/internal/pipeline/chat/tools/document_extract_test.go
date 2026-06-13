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

func TestIsExtractableDocument(t *testing.T) {
	yes := []struct{ mime, name string }{
		{"application/pdf", ""},
		{"", "a.xlsx"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", ""},
		{"application/vnd.oasis.opendocument.text", ""},
		{"text/csv", ""},
		{"", "data.csv"},
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
