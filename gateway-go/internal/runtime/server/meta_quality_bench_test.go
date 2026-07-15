package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// weakestPillars returns the n lowest, ascending.
func TestWeakestPillarsReturnsLowestScoresAscending(t *testing.T) {
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
func TestMetaQualityBenchEvidenceFormatsBaselineScoreAndWeakestPillars(t *testing.T) {
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
func TestMetaQualityBenchEvidenceReturnsEmptyWithoutBaseline(t *testing.T) {
	t.Setenv("DENEB_PROD_DIR", t.TempDir())
	s := &Server{AutonomousSubsystem: &AutonomousSubsystem{}}
	if got := s.metaQualityBenchEvidence(context.Background()); got != "" {
		t.Fatalf("missing baseline should yield empty, got %q", got)
	}
}

func TestMetaQualityBenchEvidencePrefersV3BaselineAndSnapshotDelta(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_PROD_DIR", dir)
	audit := filepath.Join(dir, "scripts", "audit")
	if err := os.MkdirAll(audit, 0o755); err != nil {
		t.Fatal(err)
	}
	baselineJSON := `{
		"overall": 50.7,
		"domains": {"structure": 50.4, "runtime": 58.5, "fitness": 43.0},
		"pillars": {"structure.change-blast": 32.0, "runtime.latency": 28.0, "fitness.feed-card": 40.0},
		"high_findings": {"structure:x": "high"}
	}`
	if err := os.WriteFile(filepath.Join(audit, "health-v3-baseline.json"), []byte(baselineJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshotJSON := `{"score":{"overall":52.0,"domains":{"structure":51.0,"runtime":58.5,"fitness":43.0}}}`
	if err := os.WriteFile(filepath.Join(audit, "health-v3-snapshot.json"), []byte(snapshotJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// v2 present but must not win when v3 exists.
	v2 := `{"overall":88.2,"pillars":{"change-locality":55.0},"high_findings":{}}`
	if err := os.WriteFile(filepath.Join(audit, "health-v2-baseline.json"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{AutonomousSubsystem: &AutonomousSubsystem{}}
	got := s.metaQualityBenchEvidence(context.Background())
	if !strings.Contains(got, "Health Bench 3.0") || !strings.Contains(got, "50.7") {
		t.Fatalf("expected v3 evidence:\n%s", got)
	}
	if !strings.Contains(got, "라이브 델타") || !strings.Contains(got, "+1.3") {
		t.Fatalf("expected live delta:\n%s", got)
	}
	if strings.Contains(got, "88.2") {
		t.Fatalf("v2 overall leaked while v3 present:\n%s", got)
	}
}
