package chat

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func suppressionEntry(name string) skills.SkillEntry {
	e := skills.SkillEntry{}
	e.Skill.Name = name
	return e
}

// captureSlog swaps the default logger for the duration of a test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// The point of the line: a skill shipped in the repo but kept out of every
// surface must SAY so, and say which mechanism did it — the operator's
// tombstone reads differently from the curator's archival.
func TestLogSuppressedSkillsSeparatesTombstoneFromArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := skills.MarkSkillDeleted("kb-interview", "테스트", time.Now()); err != nil {
		t.Fatalf("MarkSkillDeleted: %v", err)
	}

	buf := captureSlog(t)
	discovered := []skills.SkillEntry{
		suppressionEntry("kb-interview"),
		suppressionEntry("stale-thing"),
		suppressionEntry("live-one"),
	}
	logSuppressedSkills(discovered, []skills.SkillEntry{suppressionEntry("live-one")})

	got := buf.String()
	if !strings.Contains(got, "skills suppressed") {
		t.Fatalf("no suppression line: %q", got)
	}
	// The tombstone now carries its reason and age — "왜 꺼졌는지"는 이름만큼 중요하다.
	if !strings.Contains(got, "kb-interview(테스트, 0일째)") {
		t.Errorf("tombstoned skill not attributed with reason/age: %q", got)
	}
	if !strings.Contains(got, "curatorArchived=stale-thing") {
		t.Errorf("archived skill not attributed: %q", got)
	}
	if !strings.Contains(got, "discovered=3") || !strings.Contains(got, "live=1") {
		t.Errorf("counts missing: %q", got)
	}
}

// No suppression must stay silent — this runs on every catalog rebuild, and a
// line that always fires is noise nobody reads.
func TestLogSuppressedSkillsSilentWhenNothingDropped(t *testing.T) {
	buf := captureSlog(t)
	entries := []skills.SkillEntry{suppressionEntry("a"), suppressionEntry("b")}
	logSuppressedSkills(entries, entries)
	if got := buf.String(); got != "" {
		t.Errorf("logged with nothing suppressed: %q", got)
	}
}
