package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func callTool(t *testing.T, fn ToolFunc, params any) (string, error) {
	t.Helper()
	raw := testutil.Must(json.Marshal(params))
	return fn(context.Background(), json.RawMessage(raw))
}

func mustCallTool(t *testing.T, fn ToolFunc, params any) string {
	t.Helper()
	out := testutil.Must(callTool(t, fn, params))
	return out
}

// ─── ResolvePath ────────────────────────────────────────────────────────────

func TestResolvePath_absolute(t *testing.T) {
	tmp := t.TempDir()
	got := ResolvePath(tmp+"/foo.txt", tmp)
	if got != tmp+"/foo.txt" {
		t.Errorf("got %q", got)
	}
}

func TestResolvePath_relative(t *testing.T) {
	tmp := t.TempDir()
	got := ResolvePath("subdir/file.txt", tmp)
	want := filepath.Join(tmp, "subdir/file.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_traversalClamped(t *testing.T) {
	tmp := t.TempDir()
	got := ResolvePath("../../etc/passwd", tmp)
	// Must not escape the workspace root.
	if strings.HasPrefix(got, "/etc") {
		t.Errorf("path traversal not blocked: %q", got)
	}
	abs, _ := filepath.Abs(tmp)
	if !strings.HasPrefix(got, abs) {
		t.Errorf("resolved path %q not inside workspace %q", got, abs)
	}
}

func TestResolvePath_workspaceRoot(t *testing.T) {
	tmp := t.TempDir()
	// Resolving "." should return the workspace root itself.
	got := ResolvePath(".", tmp)
	abs, _ := filepath.Abs(tmp)
	if got != abs {
		t.Errorf("got %q, want %q", got, abs)
	}
}

// ─── ToolRead ───────────────────────────────────────────────────────────────

func TestToolRead_basic(t *testing.T) {
	tmp := t.TempDir()
	content := "line1\nline2\nline3\n"
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte(content), 0o644)

	out := mustCallTool(t, ToolRead(tmp), map[string]any{"file_path": "test.txt"})
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line3") {
		t.Errorf("missing lines: %q", out)
	}
	// The file ends with \n so strings.Split produces 4 elements (last empty).
	if !strings.Contains(out, "lines") {
		t.Errorf("expected line count header: %q", out)
	}
}

func TestToolRead_missingFilePath(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolRead(tmp), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing file_path")
	}
}

func TestToolRead_fileNotFound(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolRead(tmp), map[string]any{"file_path": "nonexistent.txt"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestToolRead_offsetAndLimit(t *testing.T) {
	tmp := t.TempDir()
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	path := filepath.Join(tmp, "big.txt")
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)

	out := mustCallTool(t, ToolRead(tmp), map[string]any{
		"file_path": "big.txt",
		"offset":    3,
		"limit":     3,
	})
	if !strings.Contains(out, "line3") {
		t.Errorf("expected line3: %q", out)
	}
	if !strings.Contains(out, "line5") {
		t.Errorf("expected line5: %q", out)
	}
	if strings.Contains(out, "line1\n") || strings.Contains(out, "line7") {
		t.Errorf("unexpected lines outside range: %q", out)
	}
}

func TestToolRead_truncationHint(t *testing.T) {
	tmp := t.TempDir()
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte(strings.Join(lines, "\n")), 0o644)

	out := mustCallTool(t, ToolRead(tmp), map[string]any{
		"file_path": "f.txt",
		"limit":     3,
	})
	if !strings.Contains(out, "more lines") {
		t.Errorf("expected truncation hint: %q", out)
	}
}

func TestToolRead_functionGo(t *testing.T) {
	tmp := t.TempDir()
	src := `package foo

// Greet says hello.
func Greet(name string) string {
	return "hello " + name
}
`
	os.WriteFile(filepath.Join(tmp, "foo.go"), []byte(src), 0o644)

	out := mustCallTool(t, ToolRead(tmp), map[string]any{
		"file_path": "foo.go",
		"function":  "Greet",
	})
	if !strings.Contains(out, "Greet") {
		t.Errorf("expected function name in output: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected function body: %q", out)
	}
}

func TestToolRead_functionNotFound(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "foo.go"), []byte("package foo\n"), 0o644)

	_, err := callTool(t, ToolRead(tmp), map[string]any{
		"file_path": "foo.go",
		"function":  "NonExistent",
	})
	if err == nil {
		t.Fatal("expected error for missing function")
	}
}

// ─── ToolWrite ──────────────────────────────────────────────────────────────

func TestToolWrite_basic(t *testing.T) {
	tmp := t.TempDir()
	out := mustCallTool(t, ToolWrite(tmp), map[string]any{
		"file_path": "newfile.txt",
		"content":   "hello world",
	})
	if !strings.Contains(out, "Wrote") {
		t.Errorf("unexpected output: %q", out)
	}
	data, _ := os.ReadFile(filepath.Join(tmp, "newfile.txt"))
	if string(data) != "hello world" {
		t.Errorf("file content mismatch: %q", string(data))
	}
}

func TestToolWrite_createsParentDir(t *testing.T) {
	tmp := t.TempDir()
	mustCallTool(t, ToolWrite(tmp), map[string]any{
		"file_path": "a/b/c/file.txt",
		"content":   "nested",
	})
	data := testutil.Must(os.ReadFile(filepath.Join(tmp, "a/b/c/file.txt")))
	if string(data) != "nested" {
		t.Errorf("got %q", string(data))
	}
}

func TestToolWrite_missingFilePath(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolWrite(tmp), map[string]any{"content": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestToolWrite_overwritesExisting(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	os.WriteFile(path, []byte("old"), 0o644)

	mustCallTool(t, ToolWrite(tmp), map[string]any{
		"file_path": "f.txt",
		"content":   "new",
	})
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("expected overwrite; got %q", string(data))
	}
}

// ─── ToolEdit ───────────────────────────────────────────────────────────────

func TestToolEdit_basicReplace(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	mustCallTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"old_string": "world",
		"new_string": "Go",
	})
	data, _ := os.ReadFile(path)
	if string(data) != "hello Go" {
		t.Errorf("got %q", string(data))
	}
}

func TestToolEdit_missingOldString(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("x"), 0o644)
	_, err := callTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"new_string": "y",
	})
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
}

func TestToolEdit_notFound(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("hello"), 0o644)
	_, err := callTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"old_string": "xyz",
		"new_string": "abc",
	})
	if err == nil {
		t.Fatal("expected error when old_string not found")
	}
}

func TestToolEdit_ambiguous(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("aaa"), 0o644)
	_, err := callTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"old_string": "a",
		"new_string": "b",
	})
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
}

func TestToolEdit_replaceAll(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("aaa"), 0o644)
	mustCallTool(t, ToolEdit(tmp), map[string]any{
		"file_path":   "f.txt",
		"old_string":  "a",
		"new_string":  "b",
		"replace_all": true,
	})
	data, _ := os.ReadFile(filepath.Join(tmp, "f.txt"))
	if string(data) != "bbb" {
		t.Errorf("got %q", string(data))
	}
}

func TestToolEdit_regexReplace(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("foo123bar"), 0o644)
	mustCallTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"old_string": `\d+`,
		"new_string": "NUM",
		"regex":      true,
	})
	data, _ := os.ReadFile(filepath.Join(tmp, "f.txt"))
	if string(data) != "fooNUMbar" {
		t.Errorf("got %q", string(data))
	}
}

func TestToolEdit_lineTargeted(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("foo\nfoo\n"), 0o644)
	mustCallTool(t, ToolEdit(tmp), map[string]any{
		"file_path":  "f.txt",
		"old_string": "foo",
		"new_string": "bar",
		"line":       2,
	})
	data, _ := os.ReadFile(filepath.Join(tmp, "f.txt"))
	if !strings.Contains(string(data), "foo\nbar") {
		t.Errorf("expected only line 2 changed: %q", string(data))
	}
}

// ─── findBlockEnd ───────────────────────────────────────────────────────────

func TestFindBlockEnd_simpleBraces(t *testing.T) {
	lines := []string{
		"func foo() {",
		"  return 42",
		"}",
	}
	end := findBlockEnd(lines, 0)
	if end != 2 {
		t.Errorf("got %d, want 2", end)
	}
}

func TestFindBlockEnd_noBraces(t *testing.T) {
	lines := []string{"no braces here"}
	end := findBlockEnd(lines, 0)
	// Should return a reasonable fallback (end of lines or +30).
	if end < 0 || end >= len(lines)+31 {
		t.Errorf("unexpected end: %d", end)
	}
}

func TestFindBlockEnd_nested(t *testing.T) {
	lines := []string{
		"func foo() {",
		"  if x {",
		"    return",
		"  }",
		"}",
	}
	end := findBlockEnd(lines, 0)
	if end != 4 {
		t.Errorf("got %d, want 4", end)
	}
}

// ─── readFunctionRegex ──────────────────────────────────────────────────────

func TestReadFunctionRegex_rustFn(t *testing.T) {
	lines := []string{
		"pub fn my_func(x: u32) -> u32 {",
		"    x + 1",
		"}",
	}
	out := testutil.Must(readFunctionRegex("src/lib.rs", lines, "my_func"))
	if !strings.Contains(out, "my_func") {
		t.Errorf("expected function name: %q", out)
	}
}

func TestReadFunctionRegex_notFound(t *testing.T) {
	lines := []string{"fn other() {}"}
	_, err := readFunctionRegex("lib.rs", lines, "missing")
	if err == nil {
		t.Fatal("expected error for missing symbol")
	}
}

func TestReadFunctionRegex_pythonDef(t *testing.T) {
	lines := []string{
		"def greet(name):",
		"    return 'hi ' + name",
	}
	out := testutil.Must(readFunctionRegex("script.py", lines, "greet"))
	if !strings.Contains(out, "greet") {
		t.Errorf("expected function name: %q", out)
	}
}
