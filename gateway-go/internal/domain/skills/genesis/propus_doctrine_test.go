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
		"arxiv:2606.11459",
		"arxiv:2606.15363",
		"arxiv:2605.21240",
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
	for _, want := range []string{
		"untrusted until validated",
		"evidence path",
		"same-turn self-critique",
		"external evidence",
		"failure signature",
		"Mixed frontier",
		"evolution axis",
		"exploration map",
	} {
		if !strings.Contains(strings.Join(doctrine.ProductRules(), "\n"), want) {
			t.Fatalf("doctrine lost source principle %q: %+v", want, doctrine.ProductRules())
		}
	}
}
