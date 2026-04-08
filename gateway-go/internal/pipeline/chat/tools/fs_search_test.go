package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// ─── clampInt ────────────────────────────────────────────────────────────────

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

	out := testutil.Must(callFind(t, tmp, map[string]any{"pattern": "alpha.txt"}))
	if !strings.Contains(out, "alpha.txt") {
		t.Errorf("expected alpha.txt in output: %q", out)
	}
	if strings.Contains(out, "beta.txt") {
		t.Errorf("beta.txt should not appear: %q", out)
	}
}

func TestToolFind_wildcardGlob(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "util.go"), nil, 0o644)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), nil, 0o644)

	out := testutil.Must(callFind(t, tmp, map[string]any{"pattern": "*.go"}))
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

	out := testutil.Must(callFind(t, tmp, map[string]any{"pattern": "config"}))
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
	testutil.NoError(t, err)
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
	testutil.NoError(t, err)
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
func TestToolGrep_findsMatch(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "f.go"), []byte("package main\nfunc hello() {}\n"), 0o644)

	out := testutil.Must(callGrep(t, tmp, map[string]any{"pattern": "hello"}))
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

// ─── splitGlobs ─────────────────────────────────────────────────────────────
func TestSplitGlobs_commaSeparated(t *testing.T) {
	got := splitGlobs("*.go,*.rs,*.proto")
	if len(got) != 3 || got[0] != "*.go" || got[1] != "*.rs" || got[2] != "*.proto" {
		t.Errorf("got %v, want [*.go *.rs *.proto]", got)
	}
}

func TestSplitGlobs_braceExpansion(t *testing.T) {
	got := splitGlobs("*.{go,rs,proto}")
	if len(got) != 1 || got[0] != "*.{go,rs,proto}" {
		t.Errorf("brace expansion should pass through: got %v", got)
	}
}
// ─── ToolGrep: include filter with comma-separated globs ────────────────────

func TestToolGrep_commaSeparatedInclude(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "lib.rs"), []byte("fn hello() {}\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("hello world\n"), 0o644)

	// Comma-separated include should match both Go and Rust files.
	out, err := callGrep(t, tmp, map[string]any{
		"pattern": ".",
		"include": "*.go,*.rs",
	})
	testutil.NoError(t, err)
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

// ─── normalizeFileType ─────────────────────────────────────────────────────

func TestNormalizeFileType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"go", "go"},
		{"golang", "go"},
		{"Golang", "go"},
		{"python", "py"},
		{"Python", "py"},
		{"javascript", "js"},
		{"typescript", "ts"},
		{"rust", "rust"},
		{"c++", "cpp"},
		{"shell", "sh"},
		{"bash", "sh"},
		{"yml", "yaml"},
		{"proto", "protobuf"},
		{"dockerfile", "docker"},
		{"makefile", "make"},
		{"", ""},
		{" go ", "go"},
	}
	for _, tt := range tests {
		got := normalizeFileType(tt.input)
		if got != tt.want {
			t.Errorf("normalizeFileType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ─── stripRgFlag ──────────────────────────────────────────────────────────

func TestStripRgFlag(t *testing.T) {
	args := []string{"-n", "--type", "go", "-e", "pattern", "--", "/path"}
	got := stripRgFlag(args, "--type")
	want := []string{"-n", "-e", "pattern", "--", "/path"}
	if len(got) != len(want) {
		t.Fatalf("stripRgFlag: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stripRgFlag[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
// ─── ToolGrep: fileType normalization ──────────────────────────────────────

func TestToolGrep_fileTypeNormalization(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "lib.py"), []byte("import os\n"), 0o644)

	// "golang" should be normalized to "go" and work correctly.
	out, err := callGrep(t, tmp, map[string]any{
		"pattern":  "package",
		"fileType": "golang",
	})
	testutil.NoError(t, err)
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in output: %q", out)
	}
}
// ─── hasGrepMatches ─────────────────────────────────────────────────────────

func TestHasGrepMatches(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"empty", "", false},
		{"too short", "ab", false},
		{"valid match", "src/main.go:42:func main() {}", true},
		{"context line", "src/main.go-40-package main", true},
		{"multiple lines with match", "warning: something\nsrc/main.go:42:func main() {}\n", true},
		{"no match lines", "error: cannot open file\npermission denied\n", false},
		{"files only mode", "/path/to/file.go\n/another.go\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasGrepMatches([]byte(tc.input))
			if got != tc.expect {
				t.Errorf("hasGrepMatches(%q) = %v, want %v", tc.input, got, tc.expect)
			}
		})
	}
}

// ─── rgExitCode ─────────────────────────────────────────────────────────────

