package tools

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// makeTestXLSX builds a minimal valid .xlsx workbook in memory.
func makeTestXLSX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("xl/sharedStrings.xml",
		`<?xml version="1.0"?><sst><si><t>품목</t></si><si><t>금액</t></si><si><t>계약서</t></si></sst>`)
	write("xl/worksheets/sheet1.xml",
		`<?xml version="1.0"?><worksheet><sheetData>`+
			`<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>`+
			`<row><c t="s"><v>2</v></c><c><v>1500000</v></c></row>`+
			`</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXLSXToText(t *testing.T) {
	text, err := xlsxToText(makeTestXLSX(t))
	if err != nil {
		t.Fatalf("xlsxToText: %v", err)
	}
	for _, want := range []string{"품목", "금액", "계약서", "1500000"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in extracted text:\n%s", want, text)
		}
	}
}

func TestXLSXToText_Invalid(t *testing.T) {
	if _, err := xlsxToText([]byte("this is not a zip archive")); err == nil {
		t.Error("expected an error for non-zip data")
	}
}

func TestResolveAttachment(t *testing.T) {
	atts := []gmail.AttachmentInfo{
		{Filename: "contract.pdf", AttachmentID: "a1"},
		{Filename: "invoice_2026.xlsx", AttachmentID: "a2"},
	}
	if got := resolveAttachment(atts, "2"); got == nil || got.AttachmentID != "a2" {
		t.Errorf("index select = %+v", got)
	}
	if got := resolveAttachment(atts, "contract.pdf"); got == nil || got.AttachmentID != "a1" {
		t.Errorf("exact-name select = %+v", got)
	}
	if got := resolveAttachment(atts, "invoice"); got == nil || got.AttachmentID != "a2" {
		t.Errorf("substring select = %+v", got)
	}
	if got := resolveAttachment(atts, "nonexistent"); got != nil {
		t.Errorf("no match should return nil, got %+v", got)
	}
}

func TestSaveAttachmentToDisk(t *testing.T) {
	// A traversal-style filename must be sanitized to its base component.
	path, err := saveAttachmentToDisk("../../etc/evil.pdf", []byte("hello"))
	if err != nil {
		t.Fatalf("saveAttachmentToDisk: %v", err)
	}
	defer os.Remove(path)

	if filepath.Base(path) != "evil.pdf" {
		t.Errorf("path traversal not sanitized: %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Errorf("readback = %q, %v", got, err)
	}
}
