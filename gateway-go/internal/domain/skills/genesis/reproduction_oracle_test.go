package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// Reproduction oracle (SEA Alg 8, RSI P1.5): a producer-authored case is
// adopted only when the deterministic gate confirms fails-on-original AND
// passes-on-candidate.
func TestAdoptReproductionCaseAdoptsOnlyWhenDiscriminativeAgainstOriginalAndCandidate(t *testing.T) {
	newEvolver := func(t *testing.T) *Evolver {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		return &Evolver{tracker: tr, logger: slog.Default()}
	}
	recorded := func(t *testing.T, e *Evolver, skill string) []SkillValidationCaseRecord {
		t.Helper()
		cases, err := e.tracker.RecentSkillValidationCases(skill, 10)
		if err != nil {
			t.Fatal(err)
		}
		return cases
	}
	rc := func(required ...string) *SkillValidationCaseRecord {
		return &SkillValidationCaseRecord{
			SkillName:          "sk",
			ID:                 "repro-sk-0.1.1",
			Description:        "fixes the missing verification step",
			RequiredSubstrings: required,
			Source:             "reproduction-oracle",
			FrontierTier:       "hard",
		}
	}

	t.Run("confirmed case is adopted", func(t *testing.T) {
		e := newEvolver(t)
		e.adoptReproductionCase("sk", "old body without the fix", "new body with verification step added", rc("verification step"))
		got := recorded(t, e, "sk")
		if len(got) != 1 || got[0].Source != "reproduction-oracle" {
			t.Fatalf("confirmed reproduction case not adopted: %+v", got)
		}
	})

	t.Run("passes on original — non-discriminative, dropped", func(t *testing.T) {
		e := newEvolver(t)
		e.adoptReproductionCase("sk", "body already has verification step", "new body with verification step", rc("verification step"))
		if got := recorded(t, e, "sk"); len(got) != 0 {
			t.Fatalf("non-discriminative case adopted: %+v", got)
		}
	})

	t.Run("fails on candidate — mis-authored, dropped", func(t *testing.T) {
		e := newEvolver(t)
		e.adoptReproductionCase("sk", "old body", "new body still missing it", rc("verification step"))
		if got := recorded(t, e, "sk"); len(got) != 0 {
			t.Fatalf("mis-authored case adopted: %+v", got)
		}
	})

	t.Run("vacuous case dropped", func(t *testing.T) {
		e := newEvolver(t)
		e.adoptReproductionCase("sk", "old body", "new body", rc("   "))
		if got := recorded(t, e, "sk"); len(got) != 0 {
			t.Fatalf("vacuous case adopted: %+v", got)
		}
	})

	t.Run("nil case and nil tracker are no-ops", func(t *testing.T) {
		e := newEvolver(t)
		e.adoptReproductionCase("sk", "a", "b", nil)
		(&Evolver{logger: slog.Default()}).adoptReproductionCase("sk", "a", "b", rc("x"))
	})
}

// Sidecar-provenance refresh: a materialized file that is still the pristine
// default follows the compiled default forward; a revised file never moves.
func TestMaterializeDefaultsRefreshesPristineFileButPreservesRevisedOne(t *testing.T) {
	dir := t.TempDir()
	m := generation.NewMetaArtifacts(dir, slog.Default())
	name := "prompt.md"
	v1 := strings.Repeat("v1 prompt content. ", 20)
	v2 := strings.Repeat("v2 prompt content. ", 20)
	path := filepath.Join(dir, name)
	sidecar := path + ".default-sha256"

	// First materialization writes file + provenance sidecar.
	m.MaterializeDefaults(map[string]string{name: v1})
	if got, _ := os.ReadFile(path); string(got) != v1 {
		t.Fatalf("materialized content = %q", got)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	// Pristine file follows a changed compiled default.
	m.MaterializeDefaults(map[string]string{name: v2})
	if got, _ := os.ReadFile(path); string(got) != v2 {
		t.Fatalf("pristine default was not refreshed: %q", got)
	}

	// A revised (evolved/operator-edited) file is never clobbered.
	evolved := v2 + "\noperator refinement"
	if err := os.WriteFile(path, []byte(evolved), 0o644); err != nil {
		t.Fatal(err)
	}
	m.MaterializeDefaults(map[string]string{name: strings.Repeat("v3 prompt content. ", 20)})
	if got, _ := os.ReadFile(path); string(got) != evolved {
		t.Fatalf("revised artifact was clobbered: %q", got)
	}
}

// A pre-sidecar file that diverged from the current default has unknown
// provenance — preserved, never refreshed.
func TestMaterializeDefaults_NoSidecarDivergentPreserved(t *testing.T) {
	dir := t.TempDir()
	m := generation.NewMetaArtifacts(dir, slog.Default())
	name := "prompt.md"
	legacy := strings.Repeat("legacy default from an old binary. ", 10)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	m.MaterializeDefaults(map[string]string{name: strings.Repeat("new default. ", 30)})
	if got, _ := os.ReadFile(path); string(got) != legacy {
		t.Fatalf("unknown-provenance file was clobbered: %q", got)
	}
	if _, err := os.Stat(path + ".default-sha256"); err == nil {
		t.Fatal("sidecar must not be backfilled for divergent content")
	}

	// But a pre-sidecar file that MATCHES the current default gets provenance
	// backfilled so future refreshes work.
	if err := os.WriteFile(path, []byte(strings.Repeat("new default. ", 30)), 0o644); err != nil {
		t.Fatal(err)
	}
	m.MaterializeDefaults(map[string]string{name: strings.Repeat("new default. ", 30)})
	if _, err := os.Stat(path + ".default-sha256"); err != nil {
		t.Fatalf("sidecar not backfilled for pristine match: %v", err)
	}
}
