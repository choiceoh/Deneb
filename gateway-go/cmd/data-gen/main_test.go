package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIsDeterministicAndHonorsMetadata(t *testing.T) {
	doc := map[string]any{
		"_gen": map[string]any{
			"package":    "sample",
			"imports":    []any{"time"},
			"build_tags": "tools",
		},
		"Zed": map[string]any{
			"_type":   "map[string]int",
			"_values": map[string]any{"b": float64(2), "a": float64(1)},
		},
		"Alpha": map[string]any{
			"_type":   "[]string",
			"_values": []any{"first", "second"},
		},
	}

	got := generate(prepareGeneration(doc, "data/sample.json"))
	for _, want := range []string{
		"//go:build tools", "package sample", `import "time"`,
		"var Alpha = []string{", `"first"`, `"second"`, "var Zed = map[string]int{",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated output missing %q\n%s", want, got)
		}
	}
	if strings.Index(got, `"a": 1`) > strings.Index(got, `"b": 2`) {
		t.Fatal("map keys are not sorted")
	}
	if strings.Index(got, "var Alpha") > strings.Index(got, "var Zed") {
		t.Fatal("variables are not sorted")
	}
}

func TestTypeAndValueRendering(t *testing.T) {
	if k, v := parseMapType("map[string][]int"); k != "string" || v != "[]int" {
		t.Fatalf("parseMapType = %q, %q", k, v)
	}
	if got := parseSliceType("[][]string"); got != "[]string" {
		t.Fatalf("parseSliceType = %q", got)
	}
	if got := renderValue([]any{float64(1), float64(2)}, "[]int", nil); got != "[]int{1, 2}" {
		t.Fatalf("renderValue slice = %q", got)
	}
	if got := renderValue("read", "custom.Scope", map[string]string{"read": "ScopeRead"}); got != "ScopeRead" {
		t.Fatalf("renderValue mapped = %q", got)
	}
	if got := goStr("line\n\"quoted\""); got != `"line\n\"quoted\""` {
		t.Fatalf("goStr = %q", got)
	}
}

func TestRunDataGenWritesFormattedDeterministicOutputAndSummary(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.json")
	writeDataGenInput(t, input, `{
  "_gen": {"package": "sample"},
  "Zed": {"_type": "map[string]int", "_values": {"b": 2, "a": 1}},
  "Alpha": {"_type": "[]string", "_values": ["first", "second"]}
}`)

	var first []byte
	for iteration := 1; iteration <= 2; iteration++ {
		output := filepath.Join(dir, fmt.Sprintf("generated-%d.go", iteration))
		var stdout, stderr bytes.Buffer
		if code := runDataGen([]string{"-json", input, "-out", output}, &stdout, &stderr); code != 0 {
			t.Fatalf("runDataGen iteration %d exit=%d stderr=%q", iteration, code, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("runDataGen iteration %d stderr=%q", iteration, stderr.String())
		}
		wantSummary := "wrote " + output + " (2 vars, 4 entries)\n"
		if stdout.String() != wantSummary {
			t.Fatalf("summary = %q, want %q", stdout.String(), wantSummary)
		}
		generated, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), output, generated, parser.AllErrors); err != nil {
			t.Fatalf("generated source is not formatted Go: %v\n%s", err, generated)
		}
		if !bytes.Contains(generated, []byte("// Source: "+input)) {
			t.Fatalf("generated source does not identify %q\n%s", input, generated)
		}
		info, err := os.Stat(output)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("output mode = %o, want 600", info.Mode().Perm())
		}
		if iteration == 1 {
			first = generated
		} else if !bytes.Equal(first, generated) {
			t.Fatalf("generated output changed between identical runs\nfirst:\n%s\nsecond:\n%s", first, generated)
		}
	}
}

func TestRunDataGenReportsCLIInputAndOutputErrors(t *testing.T) {
	t.Run("required flags", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := runDataGen(nil, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		if stdout.Len() != 0 || stderr.String() != dataGenUsage+"\n" {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("flag parsing", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := runDataGen([]string{"-unknown"}, &stdout, &stderr); code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined: -unknown") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := runDataGen([]string{"-h"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage of data-gen:") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "broken.json")
		writeDataGenInput(t, input, `{broken`)
		var stdout, stderr bytes.Buffer
		if code := runDataGen([]string{"-json", input, "-out", filepath.Join(dir, "out.go")}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "error: invalid character") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("output write", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "valid.json")
		writeDataGenInput(t, input, `{"Value":{"_values":{"a":"b"}}}`)
		var stdout, stderr bytes.Buffer
		if code := runDataGen([]string{"-json", input, "-out", dir}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "error:") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})
}

func TestRunDataGenPreservesGofmtWarningFallback(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "invalid-go.json")
	output := filepath.Join(dir, "invalid.go")
	writeDataGenInput(t, input, `{
  "_gen": {"package": "bad-name"},
  "Value": {"_values": {"a": "b"}}
}`)

	var stdout, stderr bytes.Buffer
	if code := runDataGen([]string{"-json", input, "-out", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "gofmt warning:") {
		t.Fatalf("stderr = %q, want gofmt warning", stderr.String())
	}
	generated, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(generated, []byte("package bad-name")) {
		t.Fatalf("unformatted fallback was not written\n%s", generated)
	}
	if want := "wrote " + output + " (1 vars, 1 entries)\n"; stdout.String() != want {
		t.Fatalf("summary = %q, want %q", stdout.String(), want)
	}
}

func writeDataGenInput(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
