package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func localSkill(dir, command string, fixedArgs ...string) SkillEntry {
	return SkillEntry{
		Skill: Skill{Name: "local-test", Dir: dir, Type: SkillTypeLocal},
		Metadata: &DenebSkillMetadata{LocalExec: &SkillLocalExec{
			Command: command,
			Args:    fixedArgs,
		}},
	}
}

func TestExecuteLocalSkillReturnsTrimmedOutputFromSkillDirectory(t *testing.T) {
	dir := t.TempDir()
	entry := localSkill(dir, "sh", "-c", `printf '%s\n\n' "$PWD"`)
	got, err := ExecuteLocalSkill(entry, "")
	if err != nil {
		t.Fatalf("ExecuteLocalSkill: %v", err)
	}
	if got != dir {
		t.Fatalf("output = %q, want skill dir %q", got, dir)
	}
}

func TestExecuteLocalSkillAppendsParsedUserArgs(t *testing.T) {
	dir := t.TempDir()
	entry := localSkill(dir, "sh", "-c", `printf '%s|%s|%s' "$1" "$2" "$3"`, "skill")
	got, err := ExecuteLocalSkill(entry, `alpha beta gamma`)
	if err != nil {
		t.Fatalf("ExecuteLocalSkill: %v", err)
	}
	if got != "alpha|beta|gamma" {
		t.Fatalf("output = %q", got)
	}
}

func TestExecuteLocalSkillIncludesStderrOnFailure(t *testing.T) {
	entry := localSkill(t.TempDir(), "sh", "-c", `printf 'specific failure' >&2; exit 7`)
	_, err := ExecuteLocalSkill(entry, "")
	if err == nil || !strings.Contains(err.Error(), "specific failure") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("error = %v, want exit status and stderr", err)
	}
}

func TestExecuteLocalSkillReportsMissingExecutable(t *testing.T) {
	entry := localSkill(t.TempDir(), filepath.Join(t.TempDir(), "missing-command"))
	_, err := ExecuteLocalSkill(entry, "")
	if err == nil || !strings.Contains(err.Error(), `skill "local-test" failed`) {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteLocalSkillErrorsWhenLocalExecConfigMissing(t *testing.T) {
	for _, entry := range []SkillEntry{
		{Skill: Skill{Name: "nil-metadata"}},
		{Skill: Skill{Name: "nil-local-exec"}, Metadata: &DenebSkillMetadata{}},
	} {
		if _, err := ExecuteLocalSkill(entry, ""); err == nil || !strings.Contains(err.Error(), "no localExec config") {
			t.Errorf("ExecuteLocalSkill(%s) error = %v", entry.Skill.Name, err)
		}
	}
}

func TestExecuteLocalSkillHonorsTimeout(t *testing.T) {
	entry := localSkill(t.TempDir(), "sh", "-c", "sleep 2")
	entry.Metadata.LocalExec.TimeoutMs = 20
	start := time.Now()
	_, err := ExecuteLocalSkill(entry, "")
	if err == nil {
		t.Fatal("timeout command unexpectedly succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %s, want prompt cancellation", elapsed)
	}
}

func TestExecuteLocalSkillPreservesStdoutBytesExceptTrailingNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte("  leading and trailing spaces  \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := localSkill(dir, "sh", "-c", "cat payload.txt")
	got, err := ExecuteLocalSkill(entry, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "  leading and trailing spaces  " {
		t.Fatalf("output = %q", got)
	}
}

func TestExecuteSystemSkillReturnsUnsupportedError(t *testing.T) {
	entry := SkillEntry{Skill: Skill{Name: "internal", Type: SkillTypeSystem}}
	if _, err := ExecuteSystemSkill(entry, "ignored"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("ExecuteSystemSkill error = %v", err)
	}
}
