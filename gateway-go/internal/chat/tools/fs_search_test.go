package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── clampInt ────────────────────────────────────────────────────────────────

func TestClampInt_below(t *testing.T) {
	if got := clampInt(-5, 0, 10); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestClampInt_above(t *testing.T) {
	if got := clampInt(20, 0, 10); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
}

func TestClampInt_within(t *testing.T) {
	if got := clampInt(5, 0, 10); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestClampInt_atBounds(t *testing.T) {
	if got := clampInt(0, 0, 10); got != 0 {
		t.Errorf("lower bound: got %d", got)
	}
	if got := clampInt(10, 0, 10); got != 10 {
		t.Errorf("upper bound: got %d", got)
	}
}

// ─── ToolFind ────────────────────────────────────────────────────────────────

func callFind(t *testing.T, defaultDir string, params map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return ToolFind(defaultDir)(context.Background(), json.RawMessage(raw))
}

func TestToolFind_matchByName(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "alpha.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "beta.txt"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{"pattern": "alpha.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alpha.txt") {
		t.Errorf("expected alpha.txt in output: %q", out)
	}
	if strings.Contains(out, "beta.txt") {
		t.Errorf("beta.txt should not appear: %q", out)
	}
}

func TestToolFind_noMatches(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file.go"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{"pattern": "*.nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No files found") {
		t.Errorf("expected no-match message: %q", out)
	}
}

func TestToolFind_missingPattern(t *testing.T) {
	tmp := t.TempDir()
	_, err := callFind(t, tmp, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestToolFind_wildcardGlob(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "util.go"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "util.go") {
		t.Errorf("expected go files: %q", out)
	}
	if strings.Contains(out, "notes.txt") {
		t.Errorf("notes.txt should not appear: %q", out)
	}
}

func TestToolFind_skipsHiddenByDefault(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmp, ".git", "config"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "visible.go"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{"pattern": "config"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not find files inside .git when showHidden=false.
	if strings.Contains(out, ".git") {
		t.Errorf("should skip hidden dir: %q", out)
	}
}

func TestToolFind_showHidden(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(tmp, ".hidden", "secret.txt"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{
		"pattern":    "secret.txt",
		"showHidden": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "secret.txt") {
		t.Errorf("expected hidden file: %q", out)
	}
}

func TestToolFind_customPath(t *testing.T) {
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "src")
	os.MkdirAll(subdir, 0o755)
	os.WriteFile(filepath.Join(subdir, "main.go"), nil, 0o644)
	// file in root should not appear when path restricts to subdir
	os.WriteFile(filepath.Join(tmp, "root.go"), nil, 0o644)

	out, err := callFind(t, tmp, map[string]any{
		"pattern": "*.go",
		"path":    "src",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go: %q", out)
	}
}

// ─── ToolGrep (basic, pattern-matching only) ─────────────────────────────────

func callGrep(t *testing.T, defaultDir string, params map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return ToolGrep(defaultDir)(context.Background(), json.RawMessage(raw))
}

func TestToolGrep_missingPattern(t *testing.T) {
	tmp := t.TempDir()
	_, err := callGrep(t, tmp, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestToolGrep_findsMatch(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.go"), []byte("package main\nfunc hello() {}\n"), 0o644)

	out, err := callGrep(t, tmp, map[string]any{"pattern": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected match: %q", out)
	}
}

// ─── groupGrepOutput ────────────────────────────────────────────────────────

func TestGroupGrepOutput_groupsByFile(t *testing.T) {
	input := "src/a.go:10:func Foo() {\nsrc/a.go:20:func Bar() {\nsrc/b.go:5:import \"fmt\"\n"
	got := groupGrepOutput(input)
	if !strings.Contains(got, "src/a.go:\n") {
		t.Errorf("expected file header: %q", got)
	}
	if !strings.Contains(got, "  10: func Foo()") {
		t.Errorf("expected indented line: %q", got)
	}
	if !strings.Contains(got, "src/b.go:\n") {
		t.Errorf("expected second file header: %q", got)
	}
	// Should NOT contain the old repeated-path format.
	if strings.Contains(got, "src/a.go:10:") {
		t.Errorf("expected paths to not repeat: %q", got)
	}
}

func TestGroupGrepOutput_singleLine(t *testing.T) {
	input := "file.go:1:only line\n"
	got := groupGrepOutput(input)
	// Single-line output should still pass through.
	if got != input {
		t.Errorf("expected passthrough: %q", got)
	}
}

func TestGroupGrepOutput_skipsSeparators(t *testing.T) {
	input := "a.go:1:match1\n--\nb.go:2:match2\n"
	got := groupGrepOutput(input)
	if strings.Contains(got, "--") {
		t.Errorf("separators should be removed: %q", got)
	}
}

// ─── splitGlobs ─────────────────────────────────────────────────────────────

func TestSplitGlobs_single(t *testing.T) {
	got := splitGlobs("*.go")
	if len(got) != 1 || got[0] != "*.go" {
		t.Errorf("expected [*.go], got %v", got)
	}
}

func TestSplitGlobs_commaSeparated(t *testing.T) {
	got := splitGlobs("*.go,*.rs,*.proto")
	if len(got) != 3 || got[0] != "*.go" || got[1] != "*.rs" || got[2] != "*.proto" {
		t.Errorf("expected [*.go *.rs *.proto], got %v", got)
	}
}

func TestSplitGlobs_braceExpansion(t *testing.T) {
	got := splitGlobs("*.{go,rs,proto}")
	if len(got) != 1 || got[0] != "*.{go,rs,proto}" {
		t.Errorf("brace expansion should pass through: got %v", got)
	}
}

func TestSplitGlobs_spacesAroundComma(t *testing.T) {
	got := splitGlobs("*.go, *.rs")
	if len(got) != 2 || got[0] != "*.go" || got[1] != "*.rs" {
		t.Errorf("expected trimmed [*.go *.rs], got %v", got)
	}
}

// ─── ToolGrep: include filter with comma-separated globs ────────────────────

func TestToolGrep_commaSeparatedInclude(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "lib.rs"), []byte("fn hello() {}\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("hello world\n"), 0o644)

	// Comma-separated include should match both Go and Rust files.
	out, err := callGrep(t, tmp, map[string]any{
		"pattern": ".",
		"include": "*.go,*.rs",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in output: %q", out)
	}
	if !strings.Contains(out, "lib.rs") {
		t.Errorf("expected lib.rs in output: %q", out)
	}
	if strings.Contains(out, "notes.txt") {
		t.Errorf("notes.txt should be excluded: %q", out)
	}
}

func TestToolGrep_noMatch(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.go"), []byte("package main\n"), 0o644)

	out, err := callGrep(t, tmp, map[string]any{"pattern": "ZZZNOMATCH"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No matches") {
		t.Errorf("expected no-match message: %q", out)
	}
}
