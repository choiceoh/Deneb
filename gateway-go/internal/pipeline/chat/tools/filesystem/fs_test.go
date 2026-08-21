package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func callTool(t *testing.T, fn toolport.ToolFunc, params any) (string, error) {
	t.Helper()
	raw := testutil.Must(json.Marshal(params))
	return fn(context.Background(), json.RawMessage(raw))
}

func mustCallTool(t *testing.T, fn toolport.ToolFunc, params any) string {
	t.Helper()
	out := testutil.Must(callTool(t, fn, params))
	return out
}

// ─── ResolvePath ────────────────────────────────────────────────────────────

func TestResolvePathReturnsAbsoluteUnchanged(t *testing.T) {
	tmp := t.TempDir()
	got := artifact.ResolvePath(tmp+"/foo.txt", tmp)
	if got != tmp+"/foo.txt" {
		t.Errorf("got %q", got)
	}
}

func TestResolvePathReturnsRelativeJoinedPath(t *testing.T) {
	tmp := t.TempDir()
	got := artifact.ResolvePath("subdir/file.txt", tmp)
	want := filepath.Join(tmp, "subdir/file.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePathClampsTraversalWithinBoundary(t *testing.T) {
	tmp := t.TempDir()
	got := artifact.ResolvePath("../../etc/passwd", tmp)
	// Must not escape the workspace root.
	if strings.HasPrefix(got, "/etc") {
		t.Errorf("path traversal not blocked: %q", got)
	}
	abs, _ := filepath.Abs(tmp)
	if !strings.HasPrefix(got, abs) {
		t.Errorf("resolved path %q not inside workspace %q", got, abs)
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

// fakeCheckpointer records the paths+reasons it saw so tests can assert the
// pre-edit hook fired.
type fakeCheckpointer struct {
	calls []struct {
		path   string
		reason string
	}
}

func (f *fakeCheckpointer) Snapshot(_ context.Context, path, reason string) error {
	f.calls = append(f.calls, struct {
		path   string
		reason string
	}{path, reason})
	return nil
}

func TestToolWrite_callsCheckpointerBeforeWrite(t *testing.T) {
	tmp := t.TempDir()
	fc := &fakeCheckpointer{}
	ctx := toolport.WithCheckpointer(context.Background(), fc)

	raw := testutil.Must(json.Marshal(map[string]any{
		"file_path": "x/y.txt",
		"content":   "hi",
	}))
	if _, err := ToolWrite(tmp)(ctx, json.RawMessage(raw)); err != nil {
		t.Fatalf("ToolWrite error: %v", err)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("expected 1 checkpoint call, got %d", len(fc.calls))
	}
	if fc.calls[0].reason != "write" {
		t.Errorf("reason = %q, want write", fc.calls[0].reason)
	}
	if !strings.HasSuffix(fc.calls[0].path, filepath.Join("x", "y.txt")) {
		t.Errorf("path = %q, want suffix x/y.txt", fc.calls[0].path)
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

// ─── ToolEdit ───────────────────────────────────────────────────────────────

func TestToolEditAcceptsPathAndOldTextAliases(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "note.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustCallTool(t, ToolEdit(tmp), map[string]any{
		"path":     path,
		"old_text": "hello",
		"new_text": "hi",
	})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi world" {
		t.Fatalf("alias edit did not apply: %q", got)
	}
}

func TestToolEditReplacesTextAndWritesFile(t *testing.T) {
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

func TestToolEditRejectsAmbiguousMatch(t *testing.T) {
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

func TestToolEditMatchesWithTolerantWhitespace(t *testing.T) {
	t.Run("unique indent mismatch auto-applies with re-indent", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "f.go")
		os.WriteFile(path, []byte("func a() {\n\tif x {\n\t\treturn 1\n\t}\n}\n"), 0o644)

		// Model reproduced the block with spaces instead of tabs.
		out := mustCallTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.go",
			"old_string": "    if x {\n        return 1\n    }",
			"new_string": "    if x {\n        return 2\n    }",
		})
		if !strings.Contains(out, "whitespace-tolerant") {
			t.Errorf("result should mention the tolerant match, got %q", out)
		}
		data, _ := os.ReadFile(path)
		want := "func a() {\n\tif x {\n\t\treturn 2\n\t}\n}\n"
		if string(data) != want {
			t.Errorf("got %q, want %q (file indentation must win)", string(data), want)
		}
	})

	t.Run("ambiguous tolerant matches report line numbers", func(t *testing.T) {
		tmp := t.TempDir()
		// "   foo" (3 spaces) is an exact substring of neither line, but both
		// lines trim-match it → the fuzzy path must report the ambiguity.
		os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("\tfoo\nbar\n  foo\n"), 0o644)
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.txt",
			"old_string": "   foo",
			"new_string": "baz",
		})
		if err == nil || !strings.Contains(err.Error(), "whitespace-tolerant matches") {
			t.Fatalf("expected ambiguity error with line numbers, got %v", err)
		}
	})

	t.Run("partial-line and true misses keep the not-found error", func(t *testing.T) {
		tmp := t.TempDir()
		os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("return foo(bar)\n"), 0o644)
		// "foo(bar)" is a substring of the line, not line-aligned → no auto-apply.
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.txt",
			"old_string": "foo(bar) ",
			"new_string": "qux",
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not-found error, got %v", err)
		}
	})

	t.Run("whitespace-only old_string never matches", func(t *testing.T) {
		tmp := t.TempDir()
		os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("a\n\t\nb\n"), 0o644)
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.txt",
			"old_string": "    ",
			"new_string": "x",
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not-found error, got %v", err)
		}
	})

	t.Run("replace_all skips the tolerant fallback", func(t *testing.T) {
		tmp := t.TempDir()
		// "  foo" (2 spaces) is not an exact substring of "\tfoo" but would
		// trim-match it — replace_all must keep exact semantics regardless.
		os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("\tfoo\n"), 0o644)
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":   "f.txt",
			"old_string":  "  foo",
			"new_string":  "bar",
			"replace_all": true,
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("replace_all must keep exact semantics, got %v", err)
		}
	})

	t.Run("conflicting indent depths refuse instead of guessing", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "f.py")
		os.WriteFile(path, []byte("def f():\n    if x:\n        return 1\n"), 0o644)
		// Model flattened the nested block: both lines share indent "" but map
		// to different file depths — any rewrite choice corrupts Python.
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.py",
			"old_string": "if x:\nreturn 1",
			"new_string": "if x:\nreturn 2",
		})
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("expected ambiguous-indentation refusal, got %v", err)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "return 1") {
			t.Errorf("file must be untouched, got %q", string(data))
		}
	})

	t.Run("CRLF files skip the tolerant path", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "f.txt")
		os.WriteFile(path, []byte("\tfoo\r\nbar\r\n"), 0o644)
		// Would trim-match "\tfoo", but splicing joins with bare \n — the
		// tolerant path must decline and leave CRLF files to exact matching.
		_, err := callTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.txt",
			"old_string": "  foo",
			"new_string": "baz",
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not-found (tolerant path declined), got %v", err)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "\r\n") {
			t.Errorf("line endings must be untouched, got %q", string(data))
		}
	})

	t.Run("unseen deeper level repeats the file indent unit", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "f.go")
		os.WriteFile(path, []byte("func a() {\n\tif x {\n\t\treturn 1\n\t}\n}\n"), 0o644)
		// old_string uses 4-space units; new_string introduces a deeper level
		// (12 spaces = 3 units) absent from the matched block. The rewrite
		// must repeat the file's tab unit (3 tabs), not concatenate tab+spaces.
		out := mustCallTool(t, ToolEdit(tmp), map[string]any{
			"file_path":  "f.go",
			"old_string": "    if x {\n        return 1\n    }",
			"new_string": "    if x {\n        if y {\n            return 3\n        }\n    }",
		})
		if !strings.Contains(out, "indentation adapted") {
			t.Errorf("expected re-indent note, got %q", out)
		}
		data, _ := os.ReadFile(path)
		want := "func a() {\n\tif x {\n\t\tif y {\n\t\t\treturn 3\n\t\t}\n\t}\n}\n"
		if string(data) != want {
			t.Errorf("got %q, want %q (no mixed tab/space indentation)", string(data), want)
		}
	})
}

func TestToolEditUpdatesAllOccurrences(t *testing.T) {
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

func TestToolEditUpdatesTextWithRegexPattern(t *testing.T) {
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

func TestToolEditUpdatesOnlyTargetedLine(t *testing.T) {
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

func TestFindBlockEndReturnsNestedBraceEnd(t *testing.T) {
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

// Batch edits apply sequentially in one call and one write.
func TestToolEditWritesBatchEditsOnce(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	os.WriteFile(path, []byte("alpha beta beta gamma"), 0o644)

	out := mustCallTool(t, ToolEdit(tmp), map[string]any{
		"file_path": "f.txt",
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "A"},
			{"old_string": "beta", "new_string": "B", "replace_all": true},
			{"old_string": "gamma", "new_string": "C"},
		},
	})
	if !strings.Contains(out, "3 edits") || !strings.Contains(out, "4 replacements") {
		t.Errorf("unexpected batch summary: %q", out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "A B B C" {
		t.Errorf("got %q", string(data))
	}
}

// A failing entry aborts the whole batch before any write (all-or-nothing).
func TestToolEditRollsBackBatchOnFailure(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	os.WriteFile(path, []byte("alpha beta"), 0o644)

	_, err := callTool(t, ToolEdit(tmp), map[string]any{
		"file_path": "f.txt",
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "A"},
			{"old_string": "MISSING", "new_string": "X"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "edits[1]") {
		t.Fatalf("expected edits[1] failure, got: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "alpha beta" {
		t.Errorf("file must be unchanged on batch failure, got %q", string(data))
	}
}
