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
