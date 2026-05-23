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

func TestDOCXToText(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>비밀유지계약서</w:t></w:r></w:p>
<w:p><w:r><w:t>계약금액: 5천만원</w:t></w:r></w:p>
</w:body>
</w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := docxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("docxToText: %v", err)
	}
	for _, want := range []string{"비밀유지계약서", "5천만원"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestPPTXToText(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addSlide := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	addSlide("ppt/slides/slide1.xml", `<?xml version="1.0"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
<p:cSld><p:spTree><p:sp><p:txBody>
<a:p><a:r><a:t>2026 사업계획</a:t></a:r></a:p>
</p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`)
	addSlide("ppt/slides/slide2.xml", `<?xml version="1.0"?>
<p:sld xmlns:p="..." xmlns:a="...">
<p:cSld><p:spTree><p:sp><p:txBody>
<a:p><a:r><a:t>매출 목표: 100억</a:t></a:r></a:p>
</p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := pptxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("pptxToText: %v", err)
	}
	for _, want := range []string{"Slide 1", "2026 사업계획", "Slide 2", "매출 목표: 100억"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
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
