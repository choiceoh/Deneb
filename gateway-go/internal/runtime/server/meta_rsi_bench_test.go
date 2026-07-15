package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetaRSIBenchEvidenceFormatsBaseline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_PROD_DIR", dir)
	audit := filepath.Join(dir, "scripts", "audit")
	if err := os.MkdirAll(audit, 0o755); err != nil {
		t.Fatal(err)
	}
	baselineJSON := `{
  "overall": 34.8,
  "domains": {"process": 39.8, "utility": 29.5},
  "pillars": {"process.acceptor-trust": 28.0, "utility.closure-land": 29.2},
  "high_findings": {}
}`
	if err := os.WriteFile(filepath.Join(audit, "rsi-bench-baseline.json"), []byte(baselineJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	got := s.metaRSIBenchEvidence(context.Background())
	if got == "" {
		t.Fatal("expected non-empty RSI bench evidence")
	}
	for _, want := range []string{"34.8", "process", "utility", "acceptor-trust"} {
		if !strings.Contains(got, want) {
			t.Fatalf("evidence missing %q:\n%s", want, got)
		}
	}
}

func TestMetaRSIBenchEvidenceEmptyWithoutBaseline(t *testing.T) {
	t.Setenv("DENEB_PROD_DIR", t.TempDir())
	s := &Server{}
	if got := s.metaRSIBenchEvidence(context.Background()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
