package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillRel = "productivity/morning-letter/SKILL.md"

// seedBundledSkill writes a skill file under bundled and returns (managed,
// bundled) skill roots where managed deliberately lacks the skill.
func seedBundledSkill(t *testing.T, body string) (managed, bundled, bundledFile string) {
	t.Helper()
	managed = t.TempDir()
	bundled = t.TempDir()
	bundledFile = filepath.Join(bundled, skillRel)
	if err := os.MkdirAll(filepath.Dir(bundledFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundledFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return managed, bundled, bundledFile
}

func TestTrySkillRootFallback(t *testing.T) {
	managed, bundled, bundledFile := seedBundledSkill(t, "# morning letter skill body")
	roots := []string{managed, bundled}

	// The model reads the bundled skill at the MANAGED root (the ~/.deneb collision).
	miss := filepath.Join(managed, skillRel)
	alt, data, ok := trySkillRootFallback(miss, roots)
	if !ok {
		t.Fatal("expected fallback to resolve the bundled skill from the managed-root path")
	}
	if alt != bundledFile {
		t.Errorf("alt=%q want %q", alt, bundledFile)
	}
	if !strings.Contains(string(data), "morning letter skill body") {
		t.Errorf("fallback returned wrong content: %q", data)
	}

	// A skill absent from EVERY root resolves nowhere.
	if _, _, ok := trySkillRootFallback(filepath.Join(managed, "nope/SKILL.md"), roots); ok {
		t.Error("expected no fallback for a skill absent from all roots")
	}
	// A path outside all skill roots is never touched (no escape).
	if _, _, ok := trySkillRootFallback("/etc/passwd", roots); ok {
		t.Error("expected no fallback for a path outside all skill roots")
	}
}

// TestToolRead_BundledSkillViaWrongRoot is the end-to-end guard: the read tool,
// asked for a bundled skill at the managed-root path the model tends to produce,
// surfaces the bundled content instead of a "no such file" error.
func TestToolRead_BundledSkillViaWrongRoot(t *testing.T) {
	managed, bundled, _ := seedBundledSkill(t, "# morning letter skill: do X then Y")
	read := ToolRead(t.TempDir(), managed, bundled)

	in := []byte(`{"file_path":"` + filepath.Join(managed, skillRel) + `"}`)
	out, err := read(context.Background(), in)
	if err != nil {
		t.Fatalf("read failed (fallback not wired?): %v", err)
	}
	if !strings.Contains(out, "do X then Y") {
		t.Errorf("read did not surface bundled skill content via fallback: %q", out)
	}
}
