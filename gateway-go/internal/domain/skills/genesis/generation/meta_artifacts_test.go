package generation

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

func TestMetaArtifacts_ProcedureRefDeterministicAndContentAddressed(t *testing.T) {
	// Nil-safe + pure-fallback: a well-formed, stable token.
	var nilM *MetaArtifacts
	ref := nilM.ProcedureRef()
	if !strings.HasPrefix(ref, "proc-") || len(ref) != len("proc-")+12 {
		t.Fatalf("ProcedureRef = %q; want proc-<12hex>", ref)
	}
	if again := NewMetaArtifacts("", discardLogger()).ProcedureRef(); again != ref {
		t.Fatalf("ProcedureRef not deterministic across pure-fallback instances: %q vs %q", ref, again)
	}

	// Materialized defaults are byte-identical to the compiled fallbacks, so the
	// ref must be unchanged — "same prompt text ⇒ same procedure".
	dir := t.TempDir()
	m := NewMetaArtifacts(dir, discardLogger())
	m.MaterializeDefaults(DefaultMetaArtifacts())
	if got := m.ProcedureRef(); got != ref {
		t.Fatalf("materialized-defaults ref = %q; want unchanged %q", got, ref)
	}

	// Revising ONE governing prompt must move the composite ref (credit
	// assignment can tell the procedure state apart).
	revised := strings.Repeat("개정된 evolve 프롬프트 본문. ", 40) // comfortably above MetaArtifactMinBytes
	if err := os.WriteFile(filepath.Join(dir, MetaEvolveSystemPrompt), []byte(revised), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.ProcedureRef(); got == ref {
		t.Fatalf("ProcedureRef unchanged after revising an artifact: %q", got)
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
func TestMetaArtifactsUnwiredLoadReturnsCompiledDefaults(t *testing.T) {
	for name, def := range DefaultMetaArtifacts() {
		if got := (*MetaArtifacts)(nil).Load(name, def); got != def {
			t.Fatalf("%s: unwired load diverges from compiled default", name)
		}
	}
}

func TestMetaArtifactsVersionReturnsHashOfEffectivePromptContent(t *testing.T) {
	// Unwired resolver: version = hash of the compiled fallback — stable.
	var nilM *MetaArtifacts
	base := nilM.Version(MetaEvolveSystemPrompt, evolveSystemPrompt)
	if len(base) != 12 {
		t.Fatalf("version length = %d, want 12", len(base))
	}
	if again := nilM.Version(MetaEvolveSystemPrompt, evolveSystemPrompt); again != base {
		t.Fatal("version must be deterministic")
	}

	// A wired dir with no file resolves to the same fallback version; a real
	// file override changes it; a short (invalid) file falls back — version
	// attribution always matches the EFFECTIVE prompt content.
	dir := t.TempDir()
	m := NewMetaArtifacts(dir, discardLogger())
	if v := m.Version(MetaEvolveSystemPrompt, evolveSystemPrompt); v != base {
		t.Fatalf("absent file version %s != fallback version %s", v, base)
	}
	long := strings.Repeat("evolved prompt content ", 20)
	if err := os.WriteFile(filepath.Join(dir, MetaEvolveSystemPrompt), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := m.Version(MetaEvolveSystemPrompt, evolveSystemPrompt); v == base {
		t.Fatal("file override must change the version")
	}
	if err := os.WriteFile(filepath.Join(dir, MetaEvolveSystemPrompt), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := m.Version(MetaEvolveSystemPrompt, evolveSystemPrompt); v != base {
		t.Fatal("short-floor fallback must report the fallback version")
	}

	vs := m.ActiveVersions(DefaultMetaArtifacts())
	if len(vs) != len(DefaultMetaArtifacts()) {
		t.Fatalf("ActiveVersions size = %d", len(vs))
	}
}
