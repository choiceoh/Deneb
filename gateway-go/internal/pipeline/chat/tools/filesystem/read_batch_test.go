package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReadFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestToolRead_BatchReadsEveryPathUnderItsOwnHeader is why file_paths exists:
// 56% of read calls sat in a run of consecutive single-read turns, while wiki
// read — the same job with a batch parameter — folds at 3.5%. One call must
// therefore carry every file, each attributable to its path.
func TestToolRead_BatchReadsEveryPathUnderItsOwnHeader(t *testing.T) {
	dir := t.TempDir()
	writeReadFixture(t, dir, "alpha.go", "package alpha\n")
	writeReadFixture(t, dir, "beta.go", "package beta\n")

	out, err := callTool(t, ToolRead(dir), map[string]any{"file_paths": []string{"alpha.go", "beta.go"}})
	if err != nil {
		t.Fatalf("batch read: %v", err)
	}
	for _, want := range []string{"[1/2] alpha.go", "package alpha", "[2/2] beta.go", "package beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("batch output missing %q:\n%s", want, out)
		}
	}
}

// TestToolRead_BatchSurvivesAnUnreadablePath: a batch that dies on its third
// path teaches the model to go back to one-at-a-time, which is exactly the
// behaviour the batch exists to remove. The bad slot reports and the rest read.
func TestToolRead_BatchSurvivesAnUnreadablePath(t *testing.T) {
	dir := t.TempDir()
	writeReadFixture(t, dir, "alpha.go", "package alpha\n")
	writeReadFixture(t, dir, "gamma.go", "package gamma\n")

	out, err := callTool(t, ToolRead(dir), map[string]any{
		"file_paths": []string{"alpha.go", "nope.go", "gamma.go"},
	})
	if err != nil {
		t.Fatalf("a missing path must not fail the batch: %v", err)
	}
	if !strings.Contains(out, "package alpha") || !strings.Contains(out, "package gamma") {
		t.Fatalf("the readable files must still come back:\n%s", out)
	}
	if !strings.Contains(out, "[2/3] nope.go") {
		t.Fatalf("the failed path must keep its slot:\n%s", out)
	}
}

// TestToolRead_BatchCapsAndSaysSo: silent truncation reads as "that was all the
// files", so the cap has to announce itself.
func TestToolRead_BatchCapsAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, readBatchCap+2)
	for i := range readBatchCap + 2 {
		name := fmt.Sprintf("f%d.txt", i)
		writeReadFixture(t, dir, name, fmt.Sprintf("body %d\n", i))
		paths = append(paths, name)
	}

	out, err := callTool(t, ToolRead(dir), map[string]any{"file_paths": paths})
	if err != nil {
		t.Fatalf("batch read: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("[%d/%d]", readBatchCap, readBatchCap)) {
		t.Errorf("want the capped batch to number up to %d:\n%s", readBatchCap, out)
	}
	if strings.Contains(out, fmt.Sprintf("f%d.txt", readBatchCap+1)) {
		t.Errorf("read past the cap:\n%s", out)
	}
	if !strings.Contains(out, "나머지는 다음 호출로") {
		t.Errorf("the cap must announce the dropped paths:\n%s", out)
	}
}

// TestToolRead_BatchRefusesInteriorModifiers: offset/limit/function/hashes
// describe ONE file's interior and mean nothing spread across several. Refusing
// beats silently ignoring them — the model would otherwise believe it had asked
// for a slice and get whole files.
func TestToolRead_BatchRefusesInteriorModifiers(t *testing.T) {
	dir := t.TempDir()
	writeReadFixture(t, dir, "alpha.go", "package alpha\n")

	for _, extra := range []map[string]any{
		{"offset": 2}, {"limit": 5}, {"function": "main"}, {"hashes": true},
	} {
		args := map[string]any{"file_paths": []string{"alpha.go"}}
		for k, v := range extra {
			args[k] = v
		}
		out, err := callTool(t, ToolRead(dir), args)
		if err != nil {
			t.Fatalf("%v: %v", extra, err)
		}
		if !strings.Contains(out, "파일 전체 읽기 전용") {
			t.Errorf("%v should be refused, got: %s", extra, out)
		}
	}
}

// TestToolRead_BatchRequiresAPath keeps an empty batch from looking like a
// successful read of nothing.
func TestToolRead_BatchRequiresAPath(t *testing.T) {
	dir := t.TempDir()
	out, err := callTool(t, ToolRead(dir), map[string]any{"file_paths": []string{"  ", ""}})
	if err != nil {
		t.Fatalf("blank batch: %v", err)
	}
	if !strings.Contains(out, "읽을 경로가 없습니다") {
		t.Fatalf("want an explicit empty-batch answer, got: %s", out)
	}
}

// TestToolRead_SingleReadStillWorks: file_path is untouched by the batch path.
func TestToolRead_SingleReadStillWorks(t *testing.T) {
	dir := t.TempDir()
	writeReadFixture(t, dir, "alpha.go", "package alpha\n")

	out, err := callTool(t, ToolRead(dir), map[string]any{"file_path": "alpha.go"})
	if err != nil {
		t.Fatalf("single read: %v", err)
	}
	if !strings.Contains(out, "package alpha") || strings.Contains(out, "[1/1]") {
		t.Fatalf("single read must stay header-free:\n%s", out)
	}
}
