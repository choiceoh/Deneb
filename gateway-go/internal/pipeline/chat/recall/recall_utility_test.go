package recall

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// TestRecordRecallUtility_RecordsWikiRowsWithRetrievalContext pins the ledger
// contract of the preflight tee: only Kind=="wiki" rows with a real page path
// are recorded as inject events, Rank is the row's 1-based position in the
// FULL injected evidence list (all kinds — the position the model saw), and
// the query label, score, and session travel with each line. The returned
// paths arm the end-of-turn citation pass.
func TestRecordRecallUtility_RecordsWikiRowsWithRetrievalContext(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	evidence := []recallEvidence{
		{Kind: "diary", Source: "2026-07-19.md", Query: "탑솔라", Score: 1.2}, // not a wiki page
		{Kind: "wiki", Source: "프로젝트/탑솔라.md", Query: "탑솔라 계약", Score: 0.95},
		{Kind: "wiki", Source: "", Query: "조직도"}, // org row without a page path
		{Kind: "wiki", Source: "인물/김.md", Query: recallCounterpartyAnchorQuery, Score: 0.9},
	}
	paths := recordRecallUtility(store, evidence, "client:main", true, nil)
	if len(paths) != 2 || paths[0] != "프로젝트/탑솔라.md" || paths[1] != "인물/김.md" {
		t.Errorf("returned paths = %v, want the two recorded wiki pages", paths)
	}

	// The ledger filename is the wiki package's stable sidecar contract
	// (.recall-hits.jsonl, sibling of the prose pages).
	f, err := os.Open(filepath.Join(dir, "wiki", ".recall-hits.jsonl"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer f.Close()
	type line struct {
		Path    string  `json:"path"`
		Event   string  `json:"event"`
		Query   string  `json:"query"`
		Rank    int     `json:"rank"`
		Score   float64 `json:"score"`
		Session string  `json:"session"`
		Top1    float64 `json:"top1"`
		Gap     float64 `json:"gap"`
		Cue     bool    `json:"cue"`
	}
	var lines []line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("malformed ledger line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	if len(lines) != 2 {
		t.Fatalf("ledger lines = %d, want 2 (diary and pathless rows excluded): %+v", len(lines), lines)
	}
	if l := lines[0]; l.Path != "프로젝트/탑솔라.md" || l.Event != wiki.RecallEventInject ||
		l.Query != "탑솔라 계약" || l.Rank != 2 || l.Score != 0.95 || l.Session != "client:main" {
		t.Errorf("first line context wrong: %+v", l)
	}
	if l := lines[1]; l.Path != "인물/김.md" || l.Query != recallCounterpartyAnchorQuery || l.Rank != 4 {
		t.Errorf("second line context wrong (rank must count non-wiki rows): %+v", l)
	}
	// Gate-shadow signals are TURN-level: every inject line of the turn carries
	// the ranked block's top-1 score, the top1−runner-up gap, and the cue flag
	// (arXiv 2607.14390 confidence-gate calibration, shadow phase — recorded,
	// never enforced here).
	for i, l := range lines {
		if l.Top1 != 1.2 || l.Gap != 0.25 || !l.Cue {
			t.Errorf("line %d gate-shadow signals wrong (top1=%v gap=%v cue=%v), want top1=1.2 gap=0.25 cue=true", i, l.Top1, l.Gap, l.Cue)
		}
	}
}
