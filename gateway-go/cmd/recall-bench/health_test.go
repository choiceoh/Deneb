package main

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestComputeLedgerUtilityReturnsCountsAndTopPages(t *testing.T) {
	u := computeLedgerUtility(map[string]wiki.RecallUsage{
		"프로젝트/a/대표.md": {Injects: 3, Reads: 1, Cites: 1}, // 5 events, used
		"프로젝트/b/대표.md": {Injects: 2},                     // exposure only
		"인물/c.md":      {Reads: 1},                       // direct model read, used
	})
	if u.distinctPages != 3 {
		t.Errorf("distinctPages = %d, want 3", u.distinctPages)
	}
	if u.totalHits != 8 {
		t.Errorf("totalHits = %d, want 8", u.totalHits)
	}
	if u.repeatPages != 2 { // a(5) and b(2) are >=2; c(1) is not
		t.Errorf("repeatPages = %d, want 2", u.repeatPages)
	}
	if u.usedPages != 2 { // a (read+cite) and c (read); b is inject-only
		t.Errorf("usedPages = %d, want 2", u.usedPages)
	}
	if len(u.topPages) == 0 || !strings.HasPrefix(u.topPages[0], "프로젝트/a/대표.md (5)") {
		t.Errorf("topPages[0] = %v, want highest-count first", u.topPages)
	}
}

func TestComputeGoldCoverageReturnsCoveredAndUncoveredProjects(t *testing.T) {
	projects := []wiki.ProjectRef{
		{Name: "당진", Path: "프로젝트/lg화학-당진/대표.md"},
		{Name: "군산", Path: "프로젝트/knk-energy/대표.md"},
	}
	cases := []goldCase{
		{ID: "g1", GoldPaths: []string{"프로젝트/lg화학-당진/대표.md"}},
	}
	cov := computeGoldCoverage(cases, projects)
	if cov.knownProjects != 2 || cov.covered != 1 {
		t.Fatalf("coverage known=%d covered=%d, want 2/1", cov.knownProjects, cov.covered)
	}
	if len(cov.uncovered) != 1 || cov.uncovered[0].Name != "군산" {
		t.Errorf("uncovered = %+v, want [군산]", cov.uncovered)
	}
}

func TestEmitGoldCandidates_ValidJSONL(t *testing.T) {
	var sb strings.Builder
	emitGoldCandidates(&sb, []wiki.ProjectRef{
		{Name: "군산태양광", Path: "프로젝트/knk-energy/대표.md", Client: "KNK"},
	})
	out := sb.String()
	// A candidate line names the project as the query and its rep page as gold.
	if !strings.Contains(out, `"question":"군산태양광"`) || !strings.Contains(out, `"gold_paths":["프로젝트/knk-energy/대표.md"]`) {
		t.Errorf("missing name candidate:\n%s", out)
	}
	// Client adds an alternate phrasing for the same page.
	if !strings.Contains(out, `"question":"KNK 군산태양광"`) {
		t.Errorf("missing client candidate:\n%s", out)
	}
}

func TestComputeRecallHealthReturnsWeightedBlendScore(t *testing.T) {
	// mrrSum/scored = 0.8 → retrieval 80; coverage 50% → 50.
	result := benchmarkResult{scored: 10, mrrSum: 8.0}
	cov := goldCoverage{knownProjects: 4, covered: 2}
	h := computeRecallHealth(result, cov)
	if int(h.retrieval+0.5) != 80 || int(h.coverage+0.5) != 50 {
		t.Fatalf("axes retrieval=%.1f coverage=%.1f, want 80/50", h.retrieval, h.coverage)
	}
	// 0.6*80 + 0.4*50 = 68
	if int(h.score+0.5) != 68 {
		t.Errorf("score = %.1f, want 68", h.score)
	}
}
