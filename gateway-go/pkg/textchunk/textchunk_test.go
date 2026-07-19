package textchunk

import (
	"strings"
	"testing"
)

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
	chunks := Split("sample.go", text, Options{TargetRunes: 80})
	if len(chunks) < 3 {
		t.Fatalf("chunks = %#v, want package plus two functions", chunks)
	}
	if chunks[1].Kind != "go-function" || chunks[1].StartLine != 3 {
		t.Fatalf("first function = %#v", chunks[1])
	}
	if chunks[0].Kind != "go-file" || !strings.Contains(chunks[0].Text, "first") || !strings.Contains(chunks[0].Text, "second") {
		t.Fatalf("file overview = %#v", chunks[0])
	}
	if chunks[2].Kind != "go-function" || chunks[2].StartLine != 7 {
		t.Fatalf("second function = %#v", chunks[2])
	}
}

func TestSplitPythonCarriesClassHierarchyIntoMethods(t *testing.T) {
	text := "import os\n\nclass Worker:\n    def run(self):\n        return 1\n\ndef top():\n    return 2\n"
	chunks := Split("worker.py", text, Options{TargetRunes: 120})
	if len(chunks) < 4 {
		t.Fatalf("chunks = %#v, want overview, class, method, function", chunks)
	}
	if chunks[0].Kind != "python-file" {
		t.Fatalf("overview = %#v", chunks[0])
	}
	if chunks[2].Kind != "python-method" || chunks[2].Heading != "Worker.run" {
		t.Fatalf("method hierarchy = %#v", chunks[2])
	}
	if chunks[3].Kind != "python-def" || chunks[3].Heading != "top" {
		t.Fatalf("top function = %#v", chunks[3])
	}
}

func TestSplitTypeScriptCarriesClassHierarchyAndTopFunction(t *testing.T) {
	text := "export class Router {\n  async recall(q: string) {\n    return q\n  }\n}\n\nconst handlers = {\n  validate(q: string) {\n    return q\n  }\n}\n\nexport function health() {\n  return true\n}\n"
	chunks := Split("router.ts", text, Options{TargetRunes: 120})
	var method, objectMethod, function *Chunk
	for i := range chunks {
		switch chunks[i].Heading {
		case "Router.recall":
			method = &chunks[i]
		case "validate":
			objectMethod = &chunks[i]
		case "health":
			function = &chunks[i]
		}
	}
	if method == nil || method.Kind != "javascript-method" {
		t.Fatalf("method not found with hierarchy: %#v", chunks)
	}
	if function == nil || function.Kind != "javascript-function" {
		t.Fatalf("top function not found: %#v", chunks)
	}
	if objectMethod == nil || objectMethod.Kind != "javascript-method" {
		t.Fatalf("unscoped object method gained a false class prefix: %#v", chunks)
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
