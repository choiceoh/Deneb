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
	wantIDs := []string{"arxiv:2602.20867", "arxiv:2510.16079", "arxiv:2507.02778", "arxiv:2606.05976"}
	gotIDs := doctrine.SourceIDs()
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("expected four source papers, got %+v", gotIDs)
	}
	for i, want := range wantIDs {
		if gotIDs[i] != want {
			t.Fatalf("source paper %d = %q, want %q", i, gotIDs[i], want)
		}
	}
	if got := doctrine.LifecycleText(); got != "observe -> propose -> validate -> genesis_or_evolve -> watch -> rollback_or_backlog" {
		t.Fatalf("unexpected lifecycle text: %q", got)
	}
	if len(doctrine.Invariants) < 5 || len(doctrine.QualityGates) < 4 || len(doctrine.ProductRules()) != 4 {
		t.Fatalf("doctrine lost product constraints: %+v", doctrine)
	}
	for _, want := range []string{
		"untrusted until validated",
		"evidence path",
		"same-turn self-critique",
		"external evidence",
	} {
		if !strings.Contains(strings.Join(doctrine.ProductRules(), "\n"), want) {
			t.Fatalf("doctrine lost source principle %q: %+v", want, doctrine.ProductRules())
		}
	}
}
