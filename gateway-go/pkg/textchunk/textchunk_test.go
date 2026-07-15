package textchunk

import "testing"

func TestSplitMarkdownPreservesHeadingAndLines(t *testing.T) {
	text := "# Project\n\nintro\n\n## Decision\n\nkeep the existing API\n"
	chunks := Split("page.md", text, Options{TargetRunes: 80})
	if len(chunks) < 2 {
		t.Fatalf("chunks = %#v, want multiple structural chunks", chunks)
	}
	found := false
	for _, chunk := range chunks {
		if chunk.Heading == "Decision" {
			found = true
			if chunk.StartLine != 5 || chunk.EndLine < 7 {
				t.Fatalf("decision lines = %d-%d, want 5-7", chunk.StartLine, chunk.EndLine)
			}
		}
	}
	if !found {
		t.Fatalf("chunks = %#v, want Decision heading", chunks)
	}
}

func TestSplitGoKeepsDeclarations(t *testing.T) {
	text := "package sample\n\nfunc first() {\n\tprintln(1)\n}\n\nfunc second() {\n\tprintln(2)\n}\n"
	chunks := Split("sample.go", text, Options{TargetRunes: 40})
	if len(chunks) < 3 {
		t.Fatalf("chunks = %#v, want package plus two functions", chunks)
	}
	if chunks[1].Kind != "go-function" || chunks[1].StartLine != 3 {
		t.Fatalf("first function = %#v", chunks[1])
	}
	if chunks[2].Kind != "go-function" || chunks[2].StartLine != 7 {
		t.Fatalf("second function = %#v", chunks[2])
	}
}

func TestSplitKotlinUsesDeclarationBoundaries(t *testing.T) {
	text := "package sample\n\nclass One {\n}\n\nfun two() = 2\n"
	chunks := Split("sample.kt", text, Options{TargetRunes: 24})
	if len(chunks) < 3 {
		t.Fatalf("chunks = %#v, want package, class, function", chunks)
	}
	if chunks[1].StartLine != 3 || chunks[2].StartLine != 6 {
		t.Fatalf("declaration starts = %d, %d", chunks[1].StartLine, chunks[2].StartLine)
	}
}
