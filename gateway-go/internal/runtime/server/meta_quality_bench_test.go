package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// weakestPillars returns the n lowest, ascending.
func TestWeakestPillars(t *testing.T) {
	pillars := map[string]float64{
		"runtime-safety":     100,
		"change-locality":    55,
		"responsibility":     52.6,
		"boundary-integrity": 74.4,
		"test-effectiveness": 92.6,
	}
	got := weakestPillars(pillars, 3)
	if len(got) != 3 {
		t.Fatalf("got %d pillars, want 3", len(got))
	}
	if got[0].name != "responsibility" || got[0].score != 52.6 {
		t.Fatalf("lowest = %+v, want responsibility/52.6", got[0])
	}
	if got[1].name != "change-locality" {
		t.Fatalf("second = %+v, want change-locality", got[1])
	}
}

// The evidence reads the baseline JSON from DENEB_PROD_DIR and formats the
// overall score + weakest pillars + accepted finding count.
func TestMetaQualityBenchEvidence_FromBaseline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_PROD_DIR", dir)
	baselinePath := filepath.Join(dir, "scripts", "audit", "health-v2-baseline.json")
	if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
		t.Fatal(err)
	}
	baselineJSON := `{
		"overall": 82.7,
		"pillars": {"change-locality": 55.0, "responsibility-cohesion": 52.6, "runtime-safety": 100.0, "test-effectiveness": 92.6, "boundary-integrity": 74.4},
		"high_findings": {"a": "high", "b": "critical"}
	}`
	if err := os.WriteFile(baselinePath, []byte(baselineJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{AutonomousSubsystem: &AutonomousSubsystem{}}
	got := s.metaQualityBenchEvidence(context.Background())
	if got == "" {
		t.Fatal("expected evidence from baseline, got empty")
	}
	if !strings.Contains(got, "82.7") {
		t.Fatalf("evidence missing overall score:\n%s", got)
	}
	if !strings.Contains(got, "2건") {
		t.Fatalf("evidence missing accepted finding count:\n%s", got)
	}
	// Weakest pillars must appear, strongest (runtime-safety 100) must not.
	if !strings.Contains(got, "responsibility-cohesion") || !strings.Contains(got, "change-locality") {
		t.Fatalf("evidence missing weakest pillars:\n%s", got)
	}
	if strings.Contains(got, "runtime-safety") {
		t.Fatalf("strongest pillar leaked into weakest list:\n%s", got)
	}
	if !strings.Contains(got, "자문") || !strings.Contains(got, "게이트 통과 여부와 무관") {
		t.Fatalf("evidence missing advisory marker:\n%s", got)
	}
}

// No source tree / no baseline → empty (quiet, not broken).
func TestMetaQualityBenchEvidence_NoBaseline(t *testing.T) {
	t.Setenv("DENEB_PROD_DIR", t.TempDir())
	s := &Server{AutonomousSubsystem: &AutonomousSubsystem{}}
	if got := s.metaQualityBenchEvidence(context.Background()); got != "" {
		t.Fatalf("missing baseline should yield empty, got %q", got)
	}
}
