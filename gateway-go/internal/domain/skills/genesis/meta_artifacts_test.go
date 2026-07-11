package genesis

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestMetaArtifacts_FallbackModes(t *testing.T) {
	var nilM *MetaArtifacts
	if got := nilM.Load(MetaEvolveSystemPrompt, "fb"); got != "fb" {
		t.Fatalf("nil receiver = %q, want fallback", got)
	}
	m := NewMetaArtifacts("", discardLogger())
	if got := m.Load(MetaEvolveSystemPrompt, "fb"); got != "fb" {
		t.Fatalf("empty dir = %q, want fallback", got)
	}
	m = NewMetaArtifacts(t.TempDir(), discardLogger())
	if got := m.Load(MetaEvolveSystemPrompt, "fb"); got != "fb" {
		t.Fatalf("absent file = %q, want fallback", got)
	}
}

func TestMetaArtifacts_LoadAndShortFloor(t *testing.T) {
	dir := t.TempDir()
	m := NewMetaArtifacts(dir, discardLogger())
	long := strings.Repeat("프롬프트 내용 ", 40) // well above the floor
	if err := os.WriteFile(filepath.Join(dir, MetaEvolveSystemPrompt), []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.Load(MetaEvolveSystemPrompt, "fb"); got != strings.TrimSpace(long) {
		t.Fatalf("file content not loaded")
	}
	// A truncated/stub artifact must degrade to the compiled fallback, not
	// run the loop on a stump.
	if err := os.WriteFile(filepath.Join(dir, MetaSkillJudgeSystemPrompt), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.Load(MetaSkillJudgeSystemPrompt, "fb"); got != "fb" {
		t.Fatalf("short file = %q, want fallback", got)
	}
}

func TestMetaArtifacts_MaterializeIsWriteIfAbsent(t *testing.T) {
	dir := t.TempDir()
	m := NewMetaArtifacts(dir, discardLogger())
	m.MaterializeDefaults(DefaultMetaArtifacts())
	for name, want := range DefaultMetaArtifacts() {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s not materialized: %v", name, err)
		}
		if string(raw) != want {
			t.Fatalf("%s not a byte-copy of the compiled default", name)
		}
	}
	// An evolved artifact must survive re-materialization (deploys).
	evolved := strings.Repeat("evolved prompt content ", 20)
	if err := os.WriteFile(filepath.Join(dir, MetaEvolveSystemPrompt), []byte(evolved), 0o644); err != nil {
		t.Fatal(err)
	}
	m.MaterializeDefaults(DefaultMetaArtifacts())
	raw, _ := os.ReadFile(filepath.Join(dir, MetaEvolveSystemPrompt))
	if string(raw) != evolved {
		t.Fatal("materialize clobbered an existing (evolved) artifact")
	}
}

// Behavior-neutral rollout: with no artifacts wired, the resolver returns the
// exact compiled-in prompt for every known artifact.
func TestMetaArtifacts_UnwiredIdentity(t *testing.T) {
	for name, def := range DefaultMetaArtifacts() {
		if got := (*MetaArtifacts)(nil).Load(name, def); got != def {
			t.Fatalf("%s: unwired load diverges from compiled default", name)
		}
	}
}
