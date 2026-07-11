package recall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// TestRecallWikiEvidence_CounterpartyAnchor verifies that naming a counterparty
// pins its 거래 원장 into the recall evidence with the anchor sentinel (exempt
// from the broadening penalty) at the counterparty anchor score.
func TestRecallWikiEvidence_CounterpartyAnchor(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if werr := store.WritePage("프로젝트/거래/대한전선.md", &wiki.Page{
		Meta: wiki.Frontmatter{Title: "대한전선", Summary: "케이블 공급 거래처"},
		Body: "- 2026-06-20: 345kV 케이블 견적 회신",
	}); werr != nil {
		t.Fatalf("WritePage: %v", werr)
	}

	evidence := recallWikiEvidence(context.Background(), store, []string{"대한전선 최근 거래 상황"}, "")
	var hit *recallEvidence
	for i := range evidence {
		if evidence[i].Query == recallCounterpartyAnchorQuery {
			hit = &evidence[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("counterparty anchor row missing: %+v", evidence)
	}
	if hit.Source != "프로젝트/거래/대한전선.md" {
		t.Errorf("anchor source = %q", hit.Source)
	}
	if hit.Score != recallCounterpartyAnchorScore {
		t.Errorf("anchor score = %v, want %v", hit.Score, recallCounterpartyAnchorScore)
	}
	if !strings.Contains(hit.Note, "거래처 원장: 대한전선") || !strings.Contains(hit.Note, "345kV") {
		t.Errorf("anchor note incomplete: %q", hit.Note)
	}

	// The broadening penalty must never demote the anchor row.
	applyBroadeningPenalty(evidence, []string{"대한전선 최근 거래", "대한전선", "거래"})
	if hit.Score != recallCounterpartyAnchorScore {
		t.Errorf("broadening penalty demoted the anchor: %v", hit.Score)
	}

	// Anchors match the RAW message, not the normalized queries: token
	// normalization strips suffix syllables that can be part of a name, so a
	// query list that lost the name must still anchor via rawMessage.
	raw := recallWikiEvidence(context.Background(), store, []string{"거래"}, "대한전선이랑 최근 거래 어떻게 됐지")
	found := false
	for _, ev := range raw {
		if ev.Query == recallCounterpartyAnchorQuery {
			found = true
		}
	}
	if !found {
		t.Errorf("raw-message anchor missing: %+v", raw)
	}
}
