package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── ToolBatchRead ─────────────────────────────────────────────────────────

func TestToolBatchRead_basic(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("alpha\nbeta\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("gamma\ndelta\n"), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "a.txt")},
			{"file_path": filepath.Join(tmp, "b.txt")},
		},
	})

	if !strings.Contains(out, "alpha") {
		t.Error("missing content from a.txt")
	}
	if !strings.Contains(out, "gamma") {
		t.Error("missing content from b.txt")
	}
	if !strings.Contains(out, "2/2 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

func TestToolBatchRead_partialFailure(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "exists.txt"), []byte("hello\n"), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "exists.txt")},
			{"file_path": filepath.Join(tmp, "missing.txt")},
		},
	})

	if !strings.Contains(out, "hello") {
		t.Error("missing content from exists.txt")
	}
	if !strings.Contains(out, "Error reading") {
		t.Error("expected error for missing.txt")
	}
	if !strings.Contains(out, "1/2 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

func TestToolBatchRead_empty(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{},
	})
	if err == nil {
		t.Error("expected error for empty files list")
	}
}

func TestToolBatchRead_withFunction(t *testing.T) {
	tmp := t.TempDir()
	goContent := `package main

func Hello() string {
	return "world"
}

func Goodbye() string {
	return "bye"
}
`
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(goContent), 0o644)

	out := mustCallTool(t, ToolBatchRead(tmp), map[string]any{
		"files": []map[string]any{
			{"file_path": filepath.Join(tmp, "main.go"), "function": "Hello"},
		},
	})

	if !strings.Contains(out, "Hello") {
		t.Error("expected Hello function in output")
	}
	if !strings.Contains(out, "1/1 files read successfully") {
		t.Errorf("unexpected summary: %s", out)
	}
}

// ─── ToolSearchAndRead ─────────────────────────────────────────────────────

func TestToolSearchAndRead_basic(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file1.txt"), []byte("foo bar baz\nqux quux\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "file2.txt"), []byte("no match here\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "file3.txt"), []byte("another foo line\n"), 0o644)

	out := mustCallTool(t, ToolSearchAndRead(tmp), map[string]any{
		"pattern":       "foo",
		"context_lines": 2,
		"max_files":     10,
	})

	if !strings.Contains(out, "foo bar baz") {
		t.Error("expected file1 content")
	}
	if !strings.Contains(out, "another foo line") {
		t.Error("expected file3 content")
	}
	if strings.Contains(out, "no match here") {
		t.Error("file2 should not appear")
	}
}

func TestToolSearchAndRead_noMatch(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello world\n"), 0o644)

	out := mustCallTool(t, ToolSearchAndRead(tmp), map[string]any{
		"pattern": "nonexistent_pattern_xyz",
	})

	if !strings.Contains(out, "No matches found") {
		t.Errorf("expected no matches: %s", out)
	}
}

func TestToolSearchAndRead_maxFiles(t *testing.T) {
	requireRg(t)
	tmp := t.TempDir()
	for i := range 5 {
		name := filepath.Join(tmp, strings.Repeat("a", i+1)+".txt")
		os.WriteFile(name, []byte("target_pattern\n"), 0o644)
	}

	out := mustCallTool(t, ToolSearchAndRead(tmp), map[string]any{
		"pattern":   "target_pattern",
		"max_files": 2,
	})

	if !strings.Contains(out, "more files not shown") {
		t.Error("expected truncation message")
	}
}

// ─── mergeRanges ───────────────────────────────────────────────────────────

func TestMergeRanges_noOverlap(t *testing.T) {
	ranges := mergeRanges([]int{5, 50}, 2, 100)
	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d", len(ranges))
	}
	// First range: lines 3-7 (0-based: 2-6)
	if ranges[0].start != 2 || ranges[0].end != 6 {
		t.Errorf("range[0] = %+v", ranges[0])
	}
}

func TestMergeRanges_overlap(t *testing.T) {
	ranges := mergeRanges([]int{5, 8}, 3, 100)
	if len(ranges) != 1 {
		t.Fatalf("expected 1 merged range, got %d", len(ranges))
	}
}

func TestMergeRanges_clampStart(t *testing.T) {
	ranges := mergeRanges([]int{1}, 5, 100)
	if ranges[0].start != 0 {
		t.Errorf("start should be clamped to 0, got %d", ranges[0].start)
	}
}

func TestMergeRanges_clampEnd(t *testing.T) {
	ranges := mergeRanges([]int{98}, 5, 100)
	if ranges[0].end != 99 {
		t.Errorf("end should be clamped to 99, got %d", ranges[0].end)
	}
}

// ─── ToolInspect ───────────────────────────────────────────────────────────

func TestToolInspect_shallow(t *testing.T) {
	tmp := t.TempDir()
	goContent := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}
`
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(goContent), 0o644)

	out := mustCallTool(t, ToolInspect(tmp), map[string]any{
		"file":  filepath.Join(tmp, "main.go"),
		"depth": "shallow",
	})

	if !strings.Contains(out, "## Stats") {
		t.Error("expected Stats section")
	}
	if !strings.Contains(out, "## Outline") {
		t.Error("expected Outline section")
	}
	if !strings.Contains(out, "## Imports") {
		t.Error("expected Imports section")
	}
	if !strings.Contains(out, "Hello") {
		t.Error("expected Hello in outline")
	}
}

func TestToolInspect_symbolAutoPromotion(t *testing.T) {
	tmp := t.TempDir()
	goContent := `package main

func Greet() string {
	return "hi"
}
`
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(goContent), 0o644)

	out := mustCallTool(t, ToolInspect(tmp), map[string]any{
		"file":   filepath.Join(tmp, "main.go"),
		"symbol": "Greet",
	})

	// Should auto-promote to symbol depth.
	if !strings.Contains(out, "depth=symbol") {
		t.Error("expected auto-promotion to symbol depth")
	}
	if !strings.Contains(out, "## Symbol Definition") {
		t.Error("expected Symbol Definition section")
	}
}

func TestToolInspect_missingFile(t *testing.T) {
	tmp := t.TempDir()
	_, err := callTool(t, ToolInspect(tmp), map[string]any{
		"file": filepath.Join(tmp, "nonexistent.go"),
	})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ─── ToolApplyPatch ────────────────────────────────────────────────────────

func TestToolApplyPatch_dryRunValid(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	original := "line1\nline2\nline3\n"
	fpath := filepath.Join(tmp, "test.txt")
	os.WriteFile(fpath, []byte(original), 0o644)
	gitAdd(t, tmp, "test.txt")
	testGitCommit(t, tmp, "initial")

	// Use strip=0 with direct file paths (no a/ b/ prefixes).
	patch := "--- test.txt\n+++ test.txt\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2_modified\n line3\n"

	out := mustCallTool(t, ToolApplyPatch(tmp), map[string]any{
		"patch":   patch,
		"strip":   0,
		"dry_run": true,
	})

	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK for valid patch: %s", out)
	}

	// Verify file is unchanged after dry run.
	data, _ := os.ReadFile(fpath)
	if string(data) != original {
		t.Error("file was modified during dry_run")
	}
}

func TestToolApplyPatch_apply(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	fpath := filepath.Join(tmp, "test.txt")
	os.WriteFile(fpath, []byte("line1\nline2\nline3\n"), 0o644)
	gitAdd(t, tmp, "test.txt")
	testGitCommit(t, tmp, "initial")

	patch := "--- test.txt\n+++ test.txt\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2_modified\n line3\n"

	out := mustCallTool(t, ToolApplyPatch(tmp), map[string]any{
		"patch": patch,
		"strip": 0,
	})

	if !strings.Contains(out, "success") {
		t.Errorf("expected success: %s", out)
	}

	data, _ := os.ReadFile(fpath)
	if !strings.Contains(string(data), "line2_modified") {
		t.Error("patch was not applied")
	}
}

func TestToolApplyPatch_invalidPatch(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	out := mustCallTool(t, ToolApplyPatch(tmp), map[string]any{
		"patch":   "not a valid patch",
		"dry_run": true,
	})

	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected FAILED for invalid patch: %s", out)
	}
}

func TestPatchContainsSymlinkMode_detectsIndexModeLine(t *testing.T) {
	patch := `diff --git a/link b/link
index 1111111..2222222 120000
--- a/link
+++ b/link
@@ -1 +1 @@
-/tmp/old
+/tmp/new
`

	if !patchContainsSymlinkMode(patch) {
		t.Fatal("expected index-mode symlink patch to be detected")
	}
}

func TestToolApplyPatch_rejectsExistingSymlinkUpdatePatch(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	linkPath := filepath.Join(tmp, "link")
	if err := os.Symlink("/tmp/old", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	gitAdd(t, tmp, "link")
	testGitCommit(t, tmp, "add symlink")

	patch := `diff --git a/link b/link
index 1111111..2222222 120000
--- a/link
+++ b/link
@@ -1 +1 @@
-/tmp/old
+/etc/passwd
`

	_, err := callTool(t, ToolApplyPatch(tmp), map[string]any{
		"patch": patch,
		"strip": 1,
	})
	if err == nil {
		t.Fatal("expected existing symlink update patch to be rejected")
	}
	if !strings.Contains(err.Error(), "symlink patches are not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}

	target, readErr := os.Readlink(linkPath)
	if readErr != nil {
		t.Fatalf("expected symlink to remain intact: %v", readErr)
	}
	if target != "/tmp/old" {
		t.Fatalf("symlink target changed unexpectedly: %q", target)
	}
}

func TestToolApplyPatch_rejectsSymlinkPatch(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	patch := `diff --git a/evil_link b/evil_link
new file mode 120000
index 0000000..1111111
--- /dev/null
+++ b/evil_link
@@ -0,0 +1 @@
+/etc/passwd
`

	_, err := callTool(t, ToolApplyPatch(tmp), map[string]any{
		"patch": patch,
		"strip": 1,
	})
	if err == nil {
		t.Fatal("expected symlink patch to be rejected")
	}
	if !strings.Contains(err.Error(), "symlink patches are not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Lstat(filepath.Join(tmp, "evil_link")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink should not be created; lstat err=%v", statErr)
	}
}

// ─── git helpers ───────────────────────────────────────────────────────────

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runCmd(t, dir, "git", "init", "-b", "main")
	runCmd(t, dir, "git", "config", "user.email", "test@test.com")
	runCmd(t, dir, "git", "config", "user.name", "Test")
	runCmd(t, dir, "git", "config", "commit.gpgsign", "false")
}

func gitAdd(t *testing.T, dir, file string) {
	t.Helper()
	runCmd(t, dir, "git", "add", file)
}

func testGitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	runCmd(t, dir, "git", "commit", "-m", msg)
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %s\n%s", name, args, err, string(out))
	}
}
