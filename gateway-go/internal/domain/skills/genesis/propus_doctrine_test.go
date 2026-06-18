package genesis

import (
	"strings"
	"testing"
)

func TestPropusDoctrinePreservesSourcePrinciples(t *testing.T) {
	doctrine := PropusDoctrine()
	if doctrine.Name != "Propus" || doctrine.Codename != "propus" || doctrine.Version == "" {
		t.Fatalf("unexpected doctrine identity: %+v", doctrine)
	}
	wantIDs := []string{
		"arxiv:2602.20867",
		"arxiv:2510.16079",
		"arxiv:2507.02778",
		"arxiv:2606.05976",
		"arxiv:2606.09498",
		"arxiv:2605.22794",
		"arxiv:2606.11459",
		"arxiv:2605.21240",
		"hermes:agent-self-evolution",
	}
	gotIDs := doctrine.SourceIDs()
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("unexpected source papers: got %+v want %+v", gotIDs, wantIDs)
	}
	for i, want := range wantIDs {
		if gotIDs[i] != want {
			t.Fatalf("source paper %d = %q, want %q", i, gotIDs[i], want)
		}
	}
	if got := doctrine.LifecycleText(); got != "observe -> propose -> validate -> genesis_or_evolve -> watch -> rollback_or_backlog" {
		t.Fatalf("unexpected lifecycle text: %q", got)
	}
	if len(doctrine.Invariants) < 9 || len(doctrine.QualityGates) < 8 || len(doctrine.ProductRules()) != len(wantIDs) {
		t.Fatalf("doctrine lost product constraints: %+v", doctrine)
	}
	filteredIDs := doctrine.FilteredSourceIDs()
	if len(filteredIDs) != 1 || filteredIDs[0] != "arxiv:2606.15363" ||
		!strings.Contains(doctrine.FilteredPapers[0].FilterReason, "Single-agent") {
		t.Fatalf("weak APEX source should be filtered, got ids=%+v papers=%+v", filteredIDs, doctrine.FilteredPapers)
	}
	if strings.Contains(strings.Join(doctrine.SourceIDs(), "\n"), "2606.15363") {
		t.Fatalf("filtered APEX source leaked into canonical sources: %+v", doctrine.SourceIDs())
	}
	for _, want := range []string{
		"untrusted until validated",
		"evidence path",
		"same-turn self-critique",
		"external evidence",
		"failure signature",
		"self-improvement coding queue",
		"Mixed frontier",
		"exploration map",
		"Hermes-style evolution",
	} {
		if !strings.Contains(strings.Join(doctrine.ProductRules(), "\n"), want) {
			t.Fatalf("doctrine lost source principle %q: %+v", want, doctrine.ProductRules())
		}
	}
	if !strings.Contains(strings.Join(doctrine.Invariants, "\n"), "hermes_style_evolution") {
		t.Fatalf("doctrine should preserve Hermes patch-first gate: %+v", doctrine.Invariants)
	}
	if !strings.Contains(strings.Join(doctrine.Invariants, "\n"), "diagnostic") {
		t.Fatalf("doctrine should keep change-axis only as diagnostic metadata: %+v", doctrine.Invariants)
	}
	if !strings.Contains(strings.Join(doctrine.QualityGates, "\n"), "under_15kb") {
		t.Fatalf("doctrine should preserve Hermes size/semantic gate: %+v", doctrine.QualityGates)
	}
	if !strings.Contains(strings.Join(doctrine.QualityGates, "\n"), "source_candidate_records") {
		t.Fatalf("doctrine should preserve MOSS source-candidate gate: %+v", doctrine.QualityGates)
	}
}
