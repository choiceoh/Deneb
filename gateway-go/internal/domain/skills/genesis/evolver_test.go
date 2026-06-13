package genesis

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// TestBackupAndRollbackSkill verifies the backup-then-restore path: after an
// evolve overwrites a skill, RollbackSkill restores the exact pre-evolve content
// from the backup, and the backup sits in a .backups subdir (out of discovery).
func TestBackupAndRollbackSkill(t *testing.T) {
	dir := t.TempDir()
	skillFile := filepath.Join(dir, "SKILL.md")
	original := "---\nname: foo\nversion: \"1.0.0\"\n---\n\n# Foo\n\noriginal body\n"
	if err := os.WriteFile(skillFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Back up, then simulate an evolve overwriting the file with a worse body.
	if err := backupSkillVersion(skillFile, original); err != nil {
		t.Fatalf("backupSkillVersion: %v", err)
	}
	if got := skillBackupPath(skillFile); filepath.Base(filepath.Dir(got)) != ".backups" {
		t.Fatalf("backup must live under .backups, got %q", got)
	}
	regressed := "---\nname: foo\nversion: \"1.0.1\"\n---\n\n# Foo\n\nregressed body\n"
	if err := os.WriteFile(skillFile, []byte(regressed), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := skills.NewCatalog(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cat.Register(skills.SkillEntry{Skill: skills.Skill{Name: "foo", FilePath: skillFile, Version: "1.0.1"}})
	e := &Evolver{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog: cat,
	}

	e.RollbackSkill("foo")

	got, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("rollback must restore the exact pre-evolve content\n got: %q\nwant: %q", got, original)
	}

	// A skill with no backup is a safe no-op (does not crash or truncate).
	cat.Register(skills.SkillEntry{Skill: skills.Skill{Name: "bar", FilePath: filepath.Join(dir, "missing", "SKILL.md")}})
	e.RollbackSkill("bar")    // no backup → no-op
	e.RollbackSkill("absent") // not in catalog → no-op
}

// TestPickCandidateJudge_AvoidsSameFamily verifies that a lightweight-produced
// candidate is judged by the teacher when one is wired (judge != producer,
// arXiv:2508.02994), and falls back to the lightweight model only when no
// teacher is available.
func TestPickCandidateJudge_AvoidsSameFamily(t *testing.T) {
	lw := &llm.Client{}
	teacher := &llm.Client{}

	withTeacher := &Evolver{llmClient: lw, model: "lightweight", teacherClient: teacher, teacherModel: "main"}
	if c, m := withTeacher.pickCandidateJudge(); c != teacher || m != "main" {
		t.Fatalf("expected teacher judge for lightweight candidate, got model=%q sameAsLightweight=%v", m, c == lw)
	}

	noTeacher := &Evolver{llmClient: lw, model: "lightweight"}
	if c, m := noTeacher.pickCandidateJudge(); c != lw || m != "lightweight" {
		t.Fatalf("expected lightweight fallback judge with no teacher, got model=%q", m)
	}
}

func TestStripEchoedFrontmatter(t *testing.T) {
	fm := "---\nname: demo\nversion: \"1.1.0\"\n---\n"
	body := "# Demo\n\n## Procedure\n- step one"
	hr := "---\njust a section divider\n---\n" + body

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain body untouched", body, body},
		{"single echoed block stripped", fm + "\n" + body, body},
		{"stacked echoed blocks stripped", fm + "\n" + fm + "\n" + body, body},
		{"divider without name key kept", hr, hr},
		{"frontmatter-only input kept", fm, fm},
		{"empty input", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripEchoedFrontmatter(tc.in); got != tc.want {
				t.Errorf("stripEchoedFrontmatter() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseAndApply_StripsEchoedFrontmatterAndBumpsVersion reproduces the
// production triple-frontmatter corruption: the LLM echoes the frontmatter
// into changes.body and returns the unchanged version. The committed file
// must contain exactly one frontmatter block with a bumped patch version.
func TestParseAndApply_StripsEchoedFrontmatterAndBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: demo\nversion: \"1.1.0\"\n---\n\n# Demo\n\nold body\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &Evolver{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		selfTest: false,
	}
	entry := &skills.SkillEntry{Skill: skills.Skill{
		Name:     "demo",
		Version:  "1.1.0",
		FilePath: path,
	}}

	// LLM response: body echoes the frontmatter, new_version is unchanged.
	resp := `{"skip":false,"changes":{"description":"d","new_version":"1.1.0","body":"---\nname: demo\nversion: \"1.1.0\"\n---\n\n# Demo\n\nnew body"}}`

	result, err := e.parseAndApply(context.Background(), resp, entry, original, &UsageStats{SkillName: "demo"})
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}
	if !result.Evolved {
		t.Fatalf("expected Evolved=true, got %+v", result)
	}
	if result.NewVersion != "1.1.1" {
		t.Errorf("expected forced patch bump to 1.1.1, got %q", result.NewVersion)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	if n := strings.Count(content, "name: demo"); n != 1 {
		t.Errorf("expected exactly 1 frontmatter block, found %d name keys:\n%s", n, content)
	}
	if !strings.Contains(content, `version: "1.1.1"`) {
		t.Errorf("expected bumped version in header:\n%s", content)
	}
	if !strings.Contains(content, "new body") {
		t.Errorf("expected rewritten body to be committed:\n%s", content)
	}
}
