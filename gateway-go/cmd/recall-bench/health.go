// health.go — recall-health: closes recall's evaluation loop.
//
// recall-bench alone scores retrieval against a hand-curated, FROZEN gold set.
// But the wiki grows every day (the dreamer adds pages), so a frozen gold set
// silently loses coverage and a recall regression on new projects goes unseen —
// recall is a static-weight retriever tuned once, with no feedback. This adds
// three loop-closing signals, all offline and read-only over a wiki COPY:
//
//   - ledger utility: reads the production recall-hit ledger (.recall-hits.jsonl,
//     the same one the dreamer scores against) so the frozen bench is grounded in
//     what recall actually surfaced in real turns;
//   - gold-set coverage: how many KNOWN projects the gold set still tests — the
//     eval-completeness gap that grows as the corpus grows (P3 verifier
//     co-evolution);
//   - a composite recall-health score so the whole thing is one trendable number.
//
// --emit-gold prints deterministic (project-name → 대표페이지) candidates for the
// uncovered projects, so the gold set can grow with the corpus by curated append.
package main

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// healthStore is the read surface recall-health needs beyond Search: the
// production recall-utility ledger and the known-project roster. The real
// *wiki.Store satisfies both; the bench's test fake stubs them.
type healthStore interface {
	RecallUsageScoreCounts(now time.Time) map[string]wiki.RecallUsage
	KnownProjects() []wiki.ProjectRef
}

// The production store is the real health surface; pin the contract so a
// signature drift on either method fails the build, not a silent runtime skip.
var _ healthStore = (*wiki.Store)(nil)

// ledgerUtility summarizes the production recall-hit ledger over the score
// window: which pages recall actually pulled into chat turns, how often, and
// how many of them the model then observably USED (read-through or answer
// citation) — exposure and use reported separately, since injection alone
// does not predict use (bridge-evidence adoption).
type ledgerUtility struct {
	distinctPages int
	totalHits     int
	repeatPages   int      // pages with >= 2 ledger events — the ones earning their keep
	usedPages     int      // pages with observed use (read/cite), not just injection
	topPages      []string // up to 5, "path (n)"
}

func computeLedgerUtility(usage map[string]wiki.RecallUsage) ledgerUtility {
	u := ledgerUtility{distinctPages: len(usage)}
	type pc struct {
		path string
		n    int
	}
	ranked := make([]pc, 0, len(usage))
	for p, us := range usage {
		n := us.Injects + us.Reads + us.Cites
		u.totalHits += n
		if n >= 2 {
			u.repeatPages++
		}
		if us.Used() {
			u.usedPages++
		}
		ranked = append(ranked, pc{p, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].path < ranked[j].path
	})
	for i, r := range ranked {
		if i >= 5 {
			break
		}
		u.topPages = append(u.topPages, fmt.Sprintf("%s (%d)", r.path, r.n))
	}
	return u
}

// goldCoverage measures how much of the live project roster the gold set still
// tests. A project is "covered" when some gold case's gold_paths path-hits its
// 대표페이지 — the same match rule the bench scores with.
type goldCoverage struct {
	knownProjects int
	covered       int
	uncovered     []wiki.ProjectRef
}

func computeGoldCoverage(cases []goldCase, projects []wiki.ProjectRef) goldCoverage {
	var golds []string
	for _, c := range cases {
		golds = append(golds, c.GoldPaths...)
	}
	cov := goldCoverage{knownProjects: len(projects)}
	for _, p := range projects {
		if projectCoveredByGold(p.Path, golds) {
			cov.covered++
		} else {
			cov.uncovered = append(cov.uncovered, p)
		}
	}
	return cov
}

func projectCoveredByGold(repPath string, golds []string) bool {
	for _, g := range golds {
		// pathHit(gold, candidate): does the gold path-segment-match the rep page.
		if pathHit(g, repPath) || pathHit(repPath, g) {
			return true
		}
	}
	return false
}

// emitGoldCandidates renders deterministic gold cases for uncovered projects:
// the project's display name is a real query a user would type, and its 대표페이지
// is the unambiguous correct answer (structure-derived ground truth, independent
// of the retriever under test). Printed for curated append to the gold set — the
// eval grows with the corpus instead of rotting. Client/Sites, when present, add
// alternate real-world query phrasings for the same page.
func emitGoldCandidates(out io.Writer, uncovered []wiki.ProjectRef) {
	fmt.Fprintf(out, "# recall-health gold candidates (%d uncovered projects) — review before appending\n", len(uncovered))
	for _, p := range uncovered {
		writeGoldCandidate(out, p.Path, p.Name, p.Name)
		if p.Client != "" && p.Client != p.Name {
			writeGoldCandidate(out, p.Path, p.Client+" "+p.Name, p.Client+"-"+p.Name)
		}
		for _, site := range p.Sites {
			if site != "" {
				writeGoldCandidate(out, p.Path, site+" "+p.Name, "site-"+p.Name)
				break // one site query is enough ground truth per project
			}
		}
	}
}

func writeGoldCandidate(out io.Writer, repPath, question, id string) {
	if question == "" || repPath == "" {
		return
	}
	// Hand-rolled to keep the output a stable, readable one-line-per-case JSONL
	// that mirrors the existing gold file; the fields are simple strings.
	fmt.Fprintf(out,
		`{"id":%q,"category":"프로젝트","question":%q,"gold_paths":[%q]}`+"\n",
		"auto-"+id, question, repPath)
}

// recallHealth is the composite: retrieval quality (how well the retriever finds
// the gold answers) weighted with eval completeness (how much of the live corpus
// the gold set still tests). Honest by construction — both axes are measured,
// never assumed; ledger utility is reported for context but not scored, since
// "more injections" is not self-evidently better.
type recallHealth struct {
	score     float64 // 0–100
	retrieval float64 // 100 * MRR
	coverage  float64 // 100 * covered/known
}

func computeRecallHealth(result benchmarkResult, cov goldCoverage) recallHealth {
	var h recallHealth
	if result.scored > 0 {
		h.retrieval = 100 * result.mrrSum / float64(result.scored)
	}
	if cov.knownProjects > 0 {
		h.coverage = 100 * float64(cov.covered) / float64(cov.knownProjects)
	}
	// Retrieval dominates (0.6): a retriever that finds the answers is the point.
	// Coverage (0.4) is the co-evolution gap — a shrinking test surface as the
	// corpus grows is a real health risk, so it is weighted, not ignored.
	h.score = 0.6*h.retrieval + 0.4*h.coverage
	return h
}

// writeRecallHealth appends the loop-closing report after the bench result line.
func writeRecallHealth(out io.Writer, util *ledgerUtility, cov goldCoverage, health recallHealth) {
	if util != nil {
		fmt.Fprintf(out, "RECALL_UTIL distinctPages=%d totalHits=%d repeatPages=%d usedPages=%d\n",
			util.distinctPages, util.totalHits, util.repeatPages, util.usedPages)
		if len(util.topPages) > 0 {
			fmt.Fprintf(out, "  top recalled: %v\n", util.topPages)
		}
	}
	fmt.Fprintf(out, "RECALL_COVERAGE known=%d covered=%d uncovered=%d (%.1f%%)\n",
		cov.knownProjects, cov.covered, len(cov.uncovered),
		safePct(cov.covered, cov.knownProjects))
	fmt.Fprintf(out, "RECALL_HEALTH score=%.1f retrieval=%.1f coverage=%.1f\n",
		health.score, health.retrieval, health.coverage)
}

func safePct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}
