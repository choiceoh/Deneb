package tools

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
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

// TestXLSXToText_MarkdownTable verifies the sheet renders as a well-formed
// markdown table (header row + separator), not a loose pipe-joined blob.
func TestXLSXToText_MarkdownTable(t *testing.T) {
	text, err := xlsxToText(makeTestXLSX(t))
	if err != nil {
		t.Fatalf("xlsxToText: %v", err)
	}
	for _, want := range []string{
		"| 품목 | 금액 |",       // header row wrapped in pipes
		"| --- | --- |",     // markdown header separator
		"| 계약서 | 1500000 |", // body row
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

// TestXLSXToText_SparseColumns is the regression guard for the alignment bug:
// a cell whose A1-ref skips earlier columns (here only C2 is present) must land
// in its true column, not slide left into column A.
func TestXLSXToText_SparseColumns(t *testing.T) {
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
		`<?xml version="1.0"?><sst><si><t>품목</t></si><si><t>수량</t></si><si><t>금액</t></si><si><t>비고</t></si></sst>`)
	write("xl/worksheets/sheet1.xml",
		`<?xml version="1.0"?><worksheet><sheetData>`+
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row>`+
			`<row r="2"><c r="C2" t="s"><v>3</v></c></row>`+ // only column C present
			`</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := xlsxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("xlsxToText: %v", err)
	}
	if !strings.Contains(text, "|  |  | 비고 |") {
		t.Errorf("sparse cell C2 did not align to column 3:\n%s", text)
	}
}

// TestDOCXTable verifies a Word table (<w:tbl>) renders as a markdown table
// rather than collapsing into a vertical list of cell values.
func TestDOCXTable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>견적 내역</w:t></w:r></w:p>
<w:tbl>
<w:tr><w:tc><w:p><w:r><w:t>품목</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>수량</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>모듈</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>100</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
</w:body>
</w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := docxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("docxToText: %v", err)
	}
	for _, want := range []string{"견적 내역", "| 품목 | 수량 |", "| --- | --- |", "| 모듈 | 100 |"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

// TestPPTXTable verifies a PowerPoint DrawingML table (<a:tbl>) renders as a
// markdown table.
func TestPPTXTable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
<p:cSld><p:spTree><p:graphicFrame><a:graphic><a:graphicData>
<a:tbl>
<a:tr><a:tc><a:txBody><a:p><a:r><a:t>지역</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>매출</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
<a:tr><a:tc><a:txBody><a:p><a:r><a:t>서울</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>50억</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
</a:tbl>
</a:graphicData></a:graphic></p:graphicFrame></p:spTree></p:cSld>
</p:sld>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := pptxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("pptxToText: %v", err)
	}
	for _, want := range []string{"| 지역 | 매출 |", "| 서울 | 50억 |"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestColIndexFromRef(t *testing.T) {
	cases := map[string]int{
		"A1": 0, "B2": 1, "Z9": 25, "AA10": 26, "AB1": 27,
		"": -1, "1": -1, "  ": -1,
	}
	for ref, want := range cases {
		if got := colIndexFromRef(ref); got != want {
			t.Errorf("colIndexFromRef(%q) = %d, want %d", ref, got, want)
		}
	}
}

func TestMdTable(t *testing.T) {
	// Ragged rows are padded; a pipe in a cell is escaped.
	got := mdTable([][]string{
		{"a", "b"},
		{"c|d"}, // shorter row + embedded pipe
	})
	want := "| a | b |\n| --- | --- |\n| c\\|d |  |"
	if got != want {
		t.Errorf("mdTable mismatch:\n got: %q\nwant: %q", got, want)
	}
	if mdTable(nil) != "" {
		t.Error("mdTable(nil) should be empty")
	}
}

// TestXLSXToText_OversizedRef is the security regression guard: a crafted cell
// ref beyond Excel's column ceiling must be skipped, not drive the column-
// padding loop into an unbounded allocation. Without the cap this would try to
// allocate hundreds of thousands of cells (and far more for longer refs).
func TestXLSXToText_OversizedRef(t *testing.T) {
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
	write("xl/sharedStrings.xml", `<?xml version="1.0"?><sst><si><t>정상</t></si></sst>`)
	write("xl/worksheets/sheet1.xml",
		`<?xml version="1.0"?><worksheet><sheetData>`+
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="ZZZZ1"><v>9</v></c></row>`+ // ZZZZ ≫ XFD
			`</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := xlsxToText(buf.Bytes())
	if err != nil {
		t.Fatalf("xlsxToText: %v", err)
	}
	if !strings.Contains(text, "정상") {
		t.Errorf("valid cell dropped:\n%s", text)
	}
}
