package media

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// quietLogger returns a slog.Logger that discards output so tests don't spam
// stderr with expected warnings.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildZip creates an in-memory zip from the given entries. Each entry is a
// (name, body) pair. This is the fixture factory for most zip tests.
func buildZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		fw, err := w.Create(e[0])
		if err != nil {
			t.Fatalf("create entry %q: %v", e[0], err)
		}
		if _, err := fw.Write([]byte(e[1])); err != nil {
			t.Fatalf("write entry %q: %v", e[0], err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestIsZipDocument(t *testing.T) {
	cases := []struct {
		name string
		doc  *telegram.Document
		want bool
	}{
		{"application/zip mime", &telegram.Document{MimeType: "application/zip", FileName: "x.zip"}, true},
		{"x-zip-compressed mime", &telegram.Document{MimeType: "application/x-zip-compressed", FileName: "x.zip"}, true},
		{"octet-stream with .zip name", &telegram.Document{MimeType: "application/octet-stream", FileName: "project.zip"}, true},
		{"uppercase extension", &telegram.Document{MimeType: "", FileName: "ARCHIVE.ZIP"}, true},
		{"pdf is not zip", &telegram.Document{MimeType: "application/pdf", FileName: "x.pdf"}, false},
		{"nil doc", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isZipDocument(tc.doc); got != tc.want {
				t.Errorf("isZipDocument(%+v) = %v, want %v", tc.doc, got, tc.want)
			}
		})
	}
}

func TestCleanZipPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"src/main.go", "src/main.go"},
		{"./src/main.go", "src/main.go"},
		{"src\\main.go", "src/main.go"}, // windows backslash
		{"/etc/passwd", ""},             // absolute path rejected
		{"../../../etc/passwd", ""},     // traversal rejected
		{"..", ""},                      // bare parent rejected
		{"", ""},                        // empty rejected
		{"src/../../etc/passwd", ""},    // traversal after normalization rejected
		{"src/./file.txt", "src/file.txt"},
	}
	for _, tc := range cases {
		if got := cleanZipPath(tc.in); got != tc.want {
			t.Errorf("cleanZipPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLooksBinaryAndUTF8(t *testing.T) {
	text := []byte("hello world\n한글 섞인 UTF-8 텍스트")
	binary := []byte{0x00, 0x01, 0x02, 0x03, 0x04}

	if looksBinary(text) {
		t.Error("plain text flagged as binary")
	}
	if !looksBinary(binary) {
		t.Error("nul-bearing bytes not flagged as binary")
	}
	if !isUTF8(text) {
		t.Error("valid UTF-8 text not recognized")
	}

	// Multibyte rune split at the boundary should not false-negative.
	korean := strings.Repeat("가", 2000) // ~6000 bytes of 3-byte runes
	if !isUTF8([]byte(korean)) {
		t.Error("long Korean text not recognized as UTF-8")
	}
}

func TestTruncateUTF8(t *testing.T) {
	// "가" is 3 bytes. Truncating to 4 must drop back to 3 so we don't split.
	s := "가나다"
	got := truncateUTF8(s, 4)
	if got != "가" {
		t.Errorf("truncateUTF8(%q, 4) = %q, want %q", s, got, "가")
	}
	if truncateUTF8("abc", 10) != "abc" {
		t.Error("truncateUTF8 changed a short string")
	}
	if truncateUTF8("abc", 0) != "abc" {
		t.Error("truncateUTF8(_, 0) should return input unchanged")
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1536, "1.5 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
	}
	for _, tc := range cases {
		if got := humanSize(tc.in); got != tc.want {
			t.Errorf("humanSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProcessZipEntries_TextAndImage(t *testing.T) {
	// PNG magic bytes so DetectMIME returns "image/png".
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	data := buildZip(t, [][2]string{
		{"README.md", "# Hello\n프로젝트 설명입니다."},
		{"src/main.go", "package main\n\nfunc main() {}\n"},
		{"assets/logo.png", string(png)},
	})

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	atts := processZipEntries(context.Background(), "project.zip", int64(len(data)), reader, quietLogger())

	if len(atts) < 4 {
		t.Fatalf("expected >=4 attachments (summary + 3 entries), got %d", len(atts))
	}

	summary := atts[0]
	if summary.Type != AttachmentTypeDocumentText {
		t.Errorf("summary type = %q, want %q", summary.Type, AttachmentTypeDocumentText)
	}
	for _, want := range []string{"project.zip", "README.md", "src/main.go", "assets/logo.png"} {
		if !strings.Contains(summary.Data, want) {
			t.Errorf("summary missing %q; got:\n%s", want, summary.Data)
		}
	}

	seen := map[string]string{}
	for _, a := range atts[1:] {
		seen[a.Name] = a.Type
	}
	if seen["project.zip/README.md"] != AttachmentTypeDocumentText {
		t.Errorf("README.md not extracted as text: %v", seen)
	}
	if seen["project.zip/src/main.go"] != AttachmentTypeDocumentText {
		t.Errorf("main.go not extracted as text: %v", seen)
	}
	if seen["project.zip/assets/logo.png"] != AttachmentTypeImage {
		t.Errorf("logo.png not extracted as image: %v", seen)
	}
}

func TestProcessZipEntries_SkipsTraversalAndBinary(t *testing.T) {
	// \x00 makes this look binary — should be listed in summary but not attached.
	data := buildZip(t, [][2]string{
		{"../../etc/passwd", "root:x:0:0"},
		{"bin/a.out", "\x7fELF\x00\x00\x00payload"},
		{"notes.txt", "hello"},
	})
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	atts := processZipEntries(context.Background(), "mixed.zip", int64(len(data)), reader, quietLogger())

	// Only notes.txt should become an attachment (plus summary).
	if len(atts) != 2 {
		t.Fatalf("expected summary + 1 text attachment, got %d", len(atts))
	}
	if atts[1].Name != "mixed.zip/notes.txt" {
		t.Errorf("unexpected attachment name %q", atts[1].Name)
	}

	// Traversal path must not appear in the summary (cleanZipPath rejects it
	// early — the record is dropped, not just marked).
	if strings.Contains(atts[0].Data, "etc/passwd") {
		t.Errorf("traversal path leaked into summary:\n%s", atts[0].Data)
	}

	// Binary entry should still be listed with a note.
	if !strings.Contains(atts[0].Data, "bin/a.out") {
		t.Errorf("binary entry missing from summary:\n%s", atts[0].Data)
	}
	if !strings.Contains(atts[0].Data, "바이너리") {
		t.Errorf("binary entry note missing from summary:\n%s", atts[0].Data)
	}
}

func TestProcessZipEntries_EmptyArchive(t *testing.T) {
	data := buildZip(t, nil)
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	atts := processZipEntries(context.Background(), "empty.zip", int64(len(data)), reader, quietLogger())

	if len(atts) != 1 {
		t.Fatalf("expected summary only, got %d attachments", len(atts))
	}
	if !strings.Contains(atts[0].Data, "항목: 0개") {
		t.Errorf("empty zip summary missing zero-count line:\n%s", atts[0].Data)
	}
}

func TestProcessZipEntries_TextBudget(t *testing.T) {
	// Build entries whose total UTF-8 bytes exceed maxZipTotalTextBytes so
	// the cumulative text cap kicks in.
	big := strings.Repeat("A", maxZipPerTextAttachment) // one full quota per file
	data := buildZip(t, [][2]string{
		{"a.txt", big},
		{"b.txt", big},
		{"c.txt", big}, // should trip the total cap before full extraction
		{"d.txt", big}, // should be skipped entirely
	})
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	atts := processZipEntries(context.Background(), "big.zip", int64(len(data)), reader, quietLogger())

	var totalText int
	for _, a := range atts[1:] {
		if a.Type == AttachmentTypeDocumentText && a.Name != "big.zip (목록)" {
			totalText += len(a.Data)
		}
	}
	if totalText > maxZipTotalTextBytes+len("\n\n[... 압축파일 전체 텍스트 한도 도달]") {
		t.Errorf("total text %d exceeds budget %d", totalText, maxZipTotalTextBytes)
	}
	if !strings.Contains(atts[0].Data, "건너뜀") && !strings.Contains(atts[0].Data, "한도") {
		t.Errorf("summary missing skip/limit notice:\n%s", atts[0].Data)
	}
}

func TestBuildZipTreeSummary_ManyEntriesTruncated(t *testing.T) {
	var records []zipEntryRecord
	for i := range maxZipTreeListing + 100 {
		records = append(records, zipEntryRecord{
			path: fmt.Sprintf("file_%d.txt", i),
			size: 100,
		})
	}
	sum := buildZipTreeSummary("huge.zip", 2, records, 0)
	if !strings.Contains(sum.Data, "더 있음") {
		t.Errorf("summary missing truncation notice when exceeding listing cap:\n%s", sum.Data)
	}
}
