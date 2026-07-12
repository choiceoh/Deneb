package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedGenesisSkill writes a genesis-nested skill under a managed root and
// returns (managedRoot, skillFile). The flat <root>/<name>/SKILL.md form —
// what stale references carry — deliberately does not exist.
func seedGenesisSkill(t *testing.T, name, body string) (managed, skillFile string) {
	t.Helper()
	managed = t.TempDir()
	skillFile = filepath.Join(managed, "genesis", "productivity", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return managed, skillFile
}

func TestTrySkillLayoutFallback(t *testing.T) {
	managed, skillFile := seedGenesisSkill(t, "project-status-briefing", "# briefing skill body")
	roots := []string{managed}

	// The stale flat form (the exact shape queued self-correction records carried).
	miss := filepath.Join(managed, "project-status-briefing", "SKILL.md")
	alt, data, ok := trySkillLayoutFallback(miss, roots)
	if !ok {
		t.Fatal("expected layout fallback to resolve the genesis-nested skill from the flat path")
	}
	if alt != skillFile {
		t.Errorf("alt=%q want %q", alt, skillFile)
	}
	if !strings.Contains(string(data), "briefing skill body") {
		t.Errorf("fallback returned wrong content: %q", data)
	}

	// Non-SKILL.md misses are never rerouted.
	if _, _, ok := trySkillLayoutFallback(filepath.Join(managed, "project-status-briefing", "notes.md"), roots); ok {
		t.Error("expected no fallback for a non-SKILL.md path")
	}
	// A path outside all skill roots is never touched (no escape).
	if _, _, ok := trySkillLayoutFallback("/etc/SKILL.md", roots); ok {
		t.Error("expected no fallback for a path outside all skill roots")
	}
	// A skill absent from every layout resolves nowhere.
	if _, _, ok := trySkillLayoutFallback(filepath.Join(managed, "nope", "SKILL.md"), roots); ok {
		t.Error("expected no fallback for a skill absent from all layouts")
	}
}

// TestToolRead_GenesisSkillViaFlatPath is the end-to-end guard for the prod
// incident (2026-07-12): a session read a genesis skill at the flat managed
// path from a stale record and got ENOENT. The read tool must surface the
// genesis-nested content instead.
func TestToolRead_GenesisSkillViaFlatPath(t *testing.T) {
	managed, _ := seedGenesisSkill(t, "project-status-briefing", "# briefing skill: gather then summarize")
	read := ToolRead(t.TempDir(), managed)

	in := []byte(`{"file_path":"` + filepath.Join(managed, "project-status-briefing", "SKILL.md") + `"}`)
	out, err := read(context.Background(), in)
	if err != nil {
		t.Fatalf("read failed (layout fallback not wired?): %v", err)
	}
	if !strings.Contains(out, "gather then summarize") {
		t.Errorf("read did not surface genesis skill content via fallback: %q", out)
	}
}

// TestToolRead_MissingSkillGetsCurationHint: when the skill exists in NO root
// and NO layout (archived/removed by curation), the error must carry the
// catalog hint instead of a bare ENOENT, so the model stops retrying path
// variants and re-checks the catalog.
func TestToolRead_MissingSkillGetsCurationHint(t *testing.T) {
	managed := t.TempDir()
	read := ToolRead(t.TempDir(), managed)

	in := []byte(`{"file_path":"` + filepath.Join(managed, "system-health-check", "SKILL.md") + `"}`)
	_, err := read(context.Background(), in)
	if err == nil {
		t.Fatal("expected an error for a skill absent everywhere")
	}
	msg := err.Error()
	if !strings.Contains(msg, "system-health-check") || !strings.Contains(msg, "archived/removed by curation") {
		t.Errorf("error lacks the curation hint: %q", msg)
	}
	// Ordinary misses outside the skill roots keep the bare error.
	plain := []byte(`{"file_path":"no-such-file.txt"}`)
	if _, err := read(context.Background(), plain); err == nil || strings.Contains(err.Error(), "curation") {
		t.Errorf("non-skill miss should not carry the curation hint: %v", err)
	}
}
