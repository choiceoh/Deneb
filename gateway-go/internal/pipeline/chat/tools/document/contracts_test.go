package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func makeContractZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCSVToMarkdownContract(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantParts []string
		wantErr   bool
	}{
		{
			name:  "basic table",
			input: "name,count\nalpha,10\nbeta,20\n",
			wantParts: []string{
				"| name | count |",
				"| --- | --- |",
				"| alpha | 10 |",
				"| beta | 20 |",
			},
		},
		{
			name:  "utf8 bom removed from first header",
			input: "\ufeff품목,수량\n모듈,100\n",
			wantParts: []string{
				"| 품목 | 수량 |",
				"| 모듈 | 100 |",
			},
		},
		{
			name:  "quoted commas stay in one cell",
			input: "name,note\nalpha,\"one, two\"\n",
			wantParts: []string{
				"| name | note |",
				"| alpha | one, two |",
			},
		},
		{
			name:  "embedded newline collapses in markdown cell",
			input: "name,note\nalpha,\"line one\nline two\"\n",
			wantParts: []string{
				"| alpha | line one line two |",
			},
		},
		{
			name:  "pipe is escaped",
			input: "name,note\nalpha,a|b\n",
			wantParts: []string{
				`| alpha | a\|b |`,
			},
		},
		{
			name:  "ragged rows padded to widest",
			input: "a,b\n1\n2,3,4\n",
			wantParts: []string{
				"| a | b |  |",
				"| 1 |  |  |",
				"| 2 | 3 | 4 |",
			},
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only blank record",
			input:   "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := csvToMarkdown([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tt.wantErr)
			}
			for _, want := range tt.wantParts {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
			if strings.ContainsRune(got, '\ufeff') {
				t.Fatalf("BOM leaked into output: %q", got)
			}
		})
	}
}

func TestCSVToMarkdownTruncatesRowsAndReportsOmission(t *testing.T) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"row", "value"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 510; i++ {
		if err := w.Write([]string{fmt.Sprintf("row-%03d", i), fmt.Sprintf("value-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	got, err := csvToMarkdown(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	// The 500-row cap includes the header, so 11 rows are omitted.
	if !strings.Contains(got, "... (11행 이하 생략)") {
		t.Fatalf("omission count missing:\n%s", got[len(got)-120:])
	}
	if !strings.Contains(got, "row-499") {
		t.Fatal("last retained row missing")
	}
	if strings.Contains(got, "row-500") || strings.Contains(got, "row-510") {
		t.Fatal("rows beyond cap leaked")
	}
}

func TestCSVToMarkdownRejectsAdversarialRaggedColumns(t *testing.T) {
	data := []byte(strings.Repeat(",", maxCSVColumns) + "\nvalue\n")
	if _, err := csvToMarkdown(data); !errors.Is(err, errDocumentExtractionLimit) {
		t.Fatalf("error = %v, want CSV column amplification limit", err)
	}
}

func TestOOXMLPartLessReturnsNaturalOrder(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		parts  []string
		want   []string
	}{
		{
			name:   "worksheets",
			prefix: "xl/worksheets/sheet",
			parts: []string{
				"xl/worksheets/sheet10.xml",
				"xl/worksheets/sheet2.xml",
				"xl/worksheets/sheet1.xml",
			},
			want: []string{
				"xl/worksheets/sheet1.xml",
				"xl/worksheets/sheet2.xml",
				"xl/worksheets/sheet10.xml",
			},
		},
		{
			name:   "slides",
			prefix: "ppt/slides/slide",
			parts: []string{
				"ppt/slides/slide20.xml",
				"ppt/slides/slide3.xml",
				"ppt/slides/slide11.xml",
				"ppt/slides/slide1.xml",
			},
			want: []string{
				"ppt/slides/slide1.xml",
				"ppt/slides/slide3.xml",
				"ppt/slides/slide11.xml",
				"ppt/slides/slide20.xml",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := append([]string(nil), tt.parts...)
			for i := 0; i < len(got); i++ {
				for j := i + 1; j < len(got); j++ {
					if ooxmlPartLess(got[j], got[i], tt.prefix) {
						got[i], got[j] = got[j], got[i]
					}
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}

	if !ooxmlPartLess("xl/worksheets/sheet1.xml", "xl/worksheets/unknown.xml", "xl/worksheets/sheet") {
		t.Fatal("numbered part must sort before malformed sibling")
	}
	if ooxmlPartLess("z.xml", "a.xml", "prefix") {
		t.Fatal("malformed parts should fall back to lexical order")
	}
}

func TestXLSXToTextPreservesNaturalSheetOrder(t *testing.T) {
	data := makeContractZip(t, map[string]string{
		"xl/worksheets/sheet10.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>ten</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml":  `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>two</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet1.xml":  `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>one</t></is></c></row></sheetData></worksheet>`,
	})
	got, err := xlsxToText(data)
	if err != nil {
		t.Fatal(err)
	}
	one := strings.Index(got, "one")
	two := strings.Index(got, "two")
	ten := strings.Index(got, "ten")
	if one < 0 || two < 0 || ten < 0 || !(one < two && two < ten) {
		t.Fatalf("sheet order scrambled:\n%s", got)
	}
}

func TestPPTXToTextPreservesNaturalSlideOrder(t *testing.T) {
	slide := func(text string) string {
		return `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	data := makeContractZip(t, map[string]string{
		"ppt/slides/slide10.xml": slide("ten"),
		"ppt/slides/slide2.xml":  slide("two"),
		"ppt/slides/slide1.xml":  slide("one"),
	})
	got, err := pptxToText(data)
	if err != nil {
		t.Fatal(err)
	}
	one := strings.Index(got, "one")
	two := strings.Index(got, "two")
	ten := strings.Index(got, "ten")
	if one < 0 || two < 0 || ten < 0 || !(one < two && two < ten) {
		t.Fatalf("slide order scrambled:\n%s", got)
	}
	if !strings.Contains(got, "### Slide 1") || !strings.Contains(got, "### Slide 2") || !strings.Contains(got, "### Slide 3") {
		t.Fatalf("logical slide numbering missing:\n%s", got)
	}
}

func TestXLSXCellValueContract(t *testing.T) {
	shared := []string{"zero", "one", "two"}
	tests := []struct {
		name string
		cell xlsxCell
		want string
	}{
		{name: "shared zero", cell: xlsxCell{Type: "s", V: "0"}, want: "zero"},
		{name: "shared trimmed index", cell: xlsxCell{Type: "s", V: " 2 "}, want: "two"},
		{name: "shared negative", cell: xlsxCell{Type: "s", V: "-1"}, want: ""},
		{name: "shared out of range", cell: xlsxCell{Type: "s", V: "3"}, want: ""},
		{name: "shared malformed", cell: xlsxCell{Type: "s", V: "x"}, want: ""},
		{name: "inline string", cell: xlsxCell{Type: "inlineStr", InlineST: "inline"}, want: "inline"},
		{name: "number", cell: xlsxCell{V: "123.45"}, want: "123.45"},
		{name: "boolean", cell: xlsxCell{Type: "b", V: "1"}, want: "1"},
		{name: "formula cached result", cell: xlsxCell{V: "99"}, want: "99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xlsxCellValue(tt.cell, shared); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestXLSXToTextReadsSharedStringsAndInlineCells(t *testing.T) {
	data := makeContractZip(t, map[string]string{
		"xl/sharedStrings.xml": `<sst><si><r><t>rich </t></r><r><t>text</t></r></si><si><t>plain</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="inlineStr"><is><t>inline</t></is></c></row>` +
			`</sheetData></worksheet>`,
	})
	got, err := xlsxToText(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rich text", "plain", "inline"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestXLSXRejectsOversizedColumnReference(t *testing.T) {
	data := makeContractZip(t, map[string]string{
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1">` +
			`<c r="XFE1" t="inlineStr"><is><t>outside</t></is></c>` +
			`<c r="A1" t="inlineStr"><is><t>inside</t></is></c>` +
			`</row></sheetData></worksheet>`,
	})
	got, err := xlsxToText(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "outside") || !strings.Contains(got, "inside") {
		t.Fatalf("oversized column handling:\n%s", got)
	}
}

func TestXLSXFailureContract(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "not zip", data: []byte("not zip"), want: "압축 해제 실패"},
		{name: "no worksheets", data: makeContractZip(t, map[string]string{"doc.txt": "x"}), want: "워크시트를 찾을 수 없습니다"},
		{name: "empty sheet", data: makeContractZip(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData></sheetData></worksheet>`}), want: "빈 워크북"},
		{name: "malformed sheet", data: makeContractZip(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet>`}), want: "빈 워크북"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := xlsxToText(tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestDOCXAndPPTXFailureContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		fn   func([]byte) (string, error)
		data []byte
		want string
	}{
		{name: "docx not zip", fn: docxToText, data: []byte("bad"), want: "압축 해제 실패"},
		{name: "docx missing part", fn: docxToText, data: makeContractZip(t, map[string]string{"other.xml": "<x/>"}), want: "word/document.xml을 찾을 수 없습니다"},
		{name: "docx empty", fn: docxToText, data: makeContractZip(t, map[string]string{"word/document.xml": "<document><body/></document>"}), want: "빈 문서"},
		{name: "pptx not zip", fn: pptxToText, data: []byte("bad"), want: "압축 해제 실패"},
		{name: "pptx no slides", fn: pptxToText, data: makeContractZip(t, map[string]string{"other.xml": "<x/>"}), want: "슬라이드를 찾을 수 없습니다"},
		{name: "pptx empty", fn: pptxToText, data: makeContractZip(t, map[string]string{"ppt/slides/slide1.xml": "<sld/>"}), want: "빈 프레젠테이션"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.fn(tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestExtractOOXMLTextContract(t *testing.T) {
	tests := []struct {
		name      string
		xml       string
		wantParts []string
	}{
		{
			name: "paragraphs",
			xml:  `<document><body><p><r><t>first</t></r></p><p><r><t>second</t></r></p></body></document>`,
			wantParts: []string{
				"first\nsecond",
			},
		},
		{
			name: "multiple runs join within paragraph",
			xml:  `<document><body><p><r><t>hello </t></r><r><t>world</t></r></p></body></document>`,
			wantParts: []string{
				"hello world",
			},
		},
		{
			name: "table becomes markdown",
			xml: `<document><body><tbl>` +
				`<tr><tc><p><r><t>name</t></r></p></tc><tc><p><r><t>value</t></r></p></tc></tr>` +
				`<tr><tc><p><r><t>alpha</t></r></p></tc><tc><p><r><t>10</t></r></p></tc></tr>` +
				`</tbl></body></document>`,
			wantParts: []string{
				"| name | value |",
				"| --- | --- |",
				"| alpha | 10 |",
			},
		},
		{
			name: "multiple cell paragraphs retain separation",
			xml: `<document><body><tbl>` +
				`<tr><tc><p><r><t>line one</t></r></p><p><r><t>line two</t></r></p></tc></tr>` +
				`</tbl></body></document>`,
			wantParts: []string{
				"| line one line two |",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOOXMLText(strings.NewReader(tt.xml))
			for _, want := range tt.wantParts {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q:\n%s", want, got)
				}
			}
		})
	}
	// Malformed XML returns the successfully decoded prefix without panicking.
	if got := extractOOXMLText(strings.NewReader(`<document><p><t>prefix</t>`)); !strings.Contains(got, "prefix") {
		t.Fatalf("malformed prefix lost: %q", got)
	}
}

func TestFileClassificationContract(t *testing.T) {
	textNames := []string{
		"file.txt",
		"file.csv",
		"file.md",
		"file.json",
		"file.xml",
		"file.log",
		"file.yaml",
		"file.yml",
	}
	for _, name := range textNames {
		if !isTextFile(name) {
			t.Errorf("%s not classified text", name)
		}
	}
	for _, name := range []string{"file", "file.pdf", "file.docx", "file.exe", "file.MD"} {
		if isTextFile(name) {
			t.Errorf("%s unexpectedly classified text", name)
		}
	}
	imageNames := []string{
		"image.png",
		"image.jpg",
		"image.jpeg",
		"image.gif",
		"image.webp",
		"image.bmp",
		"image.tif",
		"image.tiff",
	}
	for _, name := range imageNames {
		if !hasImageExt(name) {
			t.Errorf("%s not classified image", name)
		}
	}
	for _, name := range []string{"image", "image.svg", "image.pdf", "image.PNG"} {
		if hasImageExt(name) {
			t.Errorf("%s unexpectedly classified image", name)
		}
	}
}

func TestIsExtractableDocumentReturnsExpectedForEachFormat(t *testing.T) {
	tests := []struct {
		name string
		mime string
		file string
		want bool
	}{
		{name: "pdf mime", mime: "application/pdf", want: true},
		{name: "pdf filename case insensitive", file: "REPORT.PDF", want: true},
		{name: "xlsx mime", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", want: true},
		{name: "docx mime", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", want: true},
		{name: "pptx mime", mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation", want: true},
		{name: "open document", mime: "application/vnd.oasis.opendocument.text", want: true},
		{name: "legacy word", mime: "application/msword", want: true},
		{name: "legacy excel", mime: "application/vnd.ms-excel", want: true},
		{name: "legacy powerpoint", mime: "application/vnd.ms-powerpoint", want: true},
		{name: "xlsx filename", file: "book.xlsx", want: true},
		{name: "xlsm filename", file: "book.xlsm", want: true},
		{name: "docx filename", file: "doc.docx", want: true},
		{name: "pptx filename", file: "deck.pptx", want: true},
		{name: "csv mime", mime: "text/csv", want: true},
		{name: "csv filename", file: "data.csv", want: true},
		{name: "markdown mime", mime: "text/markdown", want: true},
		{name: "markdown short filename", file: "readme.md", want: true},
		{name: "markdown long filename", file: "readme.markdown", want: true},
		{name: "plain text", mime: "text/plain", file: "note.txt", want: false},
		{name: "image", mime: "image/png", file: "image.png", want: false},
		{name: "html", mime: "text/html", file: "page.html", want: false},
		{name: "unknown", mime: "application/octet-stream", file: "blob.bin", want: false},
		{name: "empty", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExtractableDocument(tt.mime, tt.file); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractDocumentTextPromotionContract(t *testing.T) {
	ctx := context.Background()
	markdown := []byte("# Title\n\nBody")
	if got, ok := ExtractDocumentText(ctx, markdown, "README.MD", ""); !ok || string(got) != string(markdown) {
		t.Fatalf("markdown = %q/%v", got, ok)
	}
	if got, ok := ExtractDocumentText(ctx, markdown, "download", "text/markdown"); ok || got != "" {
		// MIME-only markdown is recognized by IsExtractableDocument but the
		// promotion facade requires a markdown filename to distinguish raw text.
		t.Fatalf("mime-only markdown promotion changed = %q/%v", got, ok)
	}
	csvData := []byte("a,b\n1,2\n")
	if got, ok := ExtractDocumentText(ctx, csvData, "data.csv", ""); !ok || !strings.Contains(got, "| a | b |") {
		t.Fatalf("csv = %q/%v", got, ok)
	}
	for _, tt := range []struct {
		name string
		data []byte
		file string
		mime string
	}{
		{name: "plain text", data: []byte("hello"), file: "note.txt", mime: "text/plain"},
		{name: "json", data: []byte(`{"a":1}`), file: "data.json", mime: "application/json"},
		{name: "unknown", data: []byte{0, 1}, file: "blob.bin", mime: "application/octet-stream"},
		{name: "empty markdown", data: []byte("  \n"), file: "empty.md", mime: "text/markdown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := ExtractDocumentText(ctx, tt.data, tt.file, tt.mime); ok || got != "" {
				t.Fatalf("got %q/%v", got, ok)
			}
		})
	}
}

func TestMarkdownTableContract(t *testing.T) {
	tests := []struct {
		name string
		rows [][]string
		want string
	}{
		{name: "nil", rows: nil, want: ""},
		{name: "empty row", rows: [][]string{{}}, want: ""},
		{name: "one header cell", rows: [][]string{{"name"}}, want: "| name |\n| --- |"},
		{name: "header and row", rows: [][]string{{"a", "b"}, {"1", "2"}}, want: "| a | b |\n| --- | --- |\n| 1 | 2 |"},
		{name: "ragged", rows: [][]string{{"a"}, {"1", "2"}}, want: "| a |  |\n| --- | --- |\n| 1 | 2 |"},
		{name: "escape", rows: [][]string{{"a|b"}, {"line\none\t two"}}, want: "| a\\|b |\n| --- |\n| line one two |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mdTable(tt.rows); got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestColumnReferenceContract(t *testing.T) {
	tests := []struct {
		ref  string
		want int
	}{
		{ref: "", want: -1},
		{ref: "1", want: -1},
		{ref: "A1", want: 0},
		{ref: "a1", want: 0},
		{ref: "Z99", want: 25},
		{ref: "AA1", want: 26},
		{ref: "AZ1", want: 51},
		{ref: "BA1", want: 52},
		{ref: "XFD1048576", want: 16383},
		{ref: "XFE1", want: 16384},
		{ref: "ABC", want: 730},
		{ref: "$A$1", want: -1},
	}
	for _, tt := range tests {
		if got := colIndexFromRef(tt.ref); got != tt.want {
			t.Errorf("colIndexFromRef(%q) = %d, want %d", tt.ref, got, tt.want)
		}
	}
}

func TestPageTableDetectionContract(t *testing.T) {
	tests := []struct {
		name string
		page string
		want bool
	}{
		{name: "empty", page: "", want: false},
		{name: "single table row", page: "a  b  c", want: false},
		{name: "two consecutive", page: "a  b  c\n1  2  3", want: false},
		{name: "three consecutive", page: "a  b  c\n1  2  3\n4  5  6", want: true},
		{name: "gap breaks run", page: "a  b  c\n1  2  3\nprose\n4  5  6", want: false},
		{name: "single spaces are prose", page: "a b c\n1 2 3\n4 5 6", want: false},
		{name: "leading trailing spaces ignored", page: "  a  b  c  \n 1  2  3 \n 4  5  6 ", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pageHasTable(tt.page); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestColumnGapsContract(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{line: "", want: 0},
		{line: "   ", want: 0},
		{line: "a b c", want: 0},
		{line: "a  b", want: 1},
		{line: "a   b   c", want: 2},
		{line: "  a  b  ", want: 1},
		{line: "가    나      다", want: 2},
		{line: "a\tb\tc", want: 0},
	}
	for _, tt := range tests {
		if got := columnGaps(tt.line); got != tt.want {
			t.Errorf("columnGaps(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestAttachmentRenderingContract(t *testing.T) {
	attachments := []Attachment{
		{Filename: "first.txt", MimeType: "text/plain"},
		{Filename: "second.bin", MimeType: "application/octet-stream"},
		{Filename: "third.md", MimeType: "text/markdown"},
	}
	texts := []string{"first body", "", "third body"}
	got := renderAttachments(attachments, texts)
	first := strings.Index(got, "first.txt")
	second := strings.Index(got, "second.bin")
	third := strings.Index(got, "third.md")
	if first < 0 || second < 0 || third < 0 || !(first < second && second < third) {
		t.Fatalf("attachment order changed:\n%s", got)
	}
	for _, want := range []string{
		"first body",
		"텍스트를 추출하지 못했습니다",
		"third body",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("rendered output should be trimmed")
	}
}

func TestAttachmentRenderingRuneBudgets(t *testing.T) {
	long := strings.Repeat("가", attachmentPerDocRunes+50)
	attachments := []Attachment{
		{Filename: "one.txt", MimeType: "text/plain"},
		{Filename: "two.txt", MimeType: "text/plain"},
		{Filename: "three.txt", MimeType: "text/plain"},
		{Filename: "four.txt", MimeType: "text/plain"},
	}
	texts := []string{long, long, long, long}
	got := renderAttachments(attachments, texts)
	if !strings.Contains(got, "생략") {
		t.Fatal("per-document or aggregate truncation marker missing")
	}
	if utf8.RuneCountInString(got) > attachmentTotalRunes+2000 {
		t.Fatalf("rendered output far exceeds aggregate budget: %d", utf8.RuneCountInString(got))
	}
	if !strings.Contains(got, "이하 첨부는 전체 분량 한도로 생략") {
		t.Fatalf("aggregate omission marker missing:\n%s", got[len(got)-300:])
	}
}

func TestClipRunesContract(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "empty", in: "", limit: 5, want: ""},
		{name: "under", in: "abc", limit: 5, want: "abc"},
		{name: "exact", in: "abc", limit: 3, want: "abc"},
		{name: "ascii over", in: "abcdef", limit: 3, want: "abc …(생략 — 전체가 필요하면 더 좁은 첨부 선택자로 다시 요청하세요.)"},
		{name: "unicode over", in: "가나다라", limit: 2, want: "가나 …(생략 — 전체가 필요하면 더 좁은 첨부 선택자로 다시 요청하세요.)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clipRunes(tt.in, tt.limit); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractAttachmentsEmptyAndTextOrder(t *testing.T) {
	if got := ExtractAttachments(context.Background(), nil); got != "" {
		t.Fatalf("nil attachments = %q", got)
	}
	attachments := []Attachment{
		{Filename: "a.txt", MimeType: "text/plain", Bytes: []byte("alpha")},
		{Filename: "b.md", MimeType: "text/markdown", Bytes: []byte("# beta")},
		{Filename: "c.json", MimeType: "application/json", Bytes: []byte(`{"gamma":true}`)},
	}
	got := ExtractAttachments(context.Background(), attachments)
	a := strings.Index(got, "alpha")
	b := strings.Index(got, "# beta")
	c := strings.Index(got, `{"gamma":true}`)
	if a < 0 || b < 0 || c < 0 || !(a < b && b < c) {
		t.Fatalf("extraction order changed:\n%s", got)
	}
}

func TestFirstLineContract(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "one", want: "one"},
		{in: "one\ntwo", want: "one"},
		{in: "  one  \n two", want: "one"},
		{in: "one\r\ntwo", want: "one"},
	}
	for _, tt := range tests {
		if got := firstLine(tt.in); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatHelpersContract(t *testing.T) {
	for _, tt := range []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 1, want: "1 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
	} {
		if got := formatBytes(tt.bytes); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
	if got := truncate("가나다라", 3); got != "가나다..." {
		t.Fatalf("truncate unicode = %q", got)
	}
}
