// recall_trs.go — transfer-reliability demotion for wiki search ranking
// (SMA review adoption, arXiv 2608.12743, 2026-08-25).
//
// The recall-hits ledger already distinguishes EXPOSURE (inject: the preflight
// surfaced a page as evidence) from USE (read/cite: the model observably acted
// on it), and the dream cycle consumes that split — but ranking never did: a
// page the model has ignored twenty times ranks exactly as it did the first
// time. SMA's transfer-reliability score closes that loop: calibrate each
// stored item's retrieval rank by whether its past retrievals actually
// transferred.
//
// Deliberately DEMOTE-ONLY. Boosting used pages creates the classic feedback
// loop — served pages get more chances to be used, which earns more boost,
// which crowds out never-served pages (rich-get-richer). Demotion has no such
// loop: a demoted page that stops being served stops accumulating evidence
// against itself, and any single observed use resets it to neutral. It also
// matches the house style — validityFactor is a demotion ladder too.
//
// Evidence bar: a page is demoted only after trsMinExposures injections in the
// score window with ZERO uses. Below that the sample is noise; one read or
// cite at any point in the window is full rehabilitation. The floor keeps a
// chronically ignored page above archived pages (0.3) — it is current, just
// unpersuasive — while it loses ties against equally matched pages that earn
// their exposure.
package wiki

import (
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// trsMinExposures is the evidence bar: fewer window injections than this
	// and the page keeps factor 1.0 regardless of use.
	trsMinExposures = 8
	// trsSaturationExposures is where the demotion bottoms out at trsFloor.
	trsSaturationExposures = 24
	// trsFloor is the maximum demotion. Chosen against the validityFactor
	// ladder: above archived (0.3) and above person stubs (0.45) — an ignored
	// page is still a current, written page; it only loses ties against
	// equally matched pages that earn their exposure.
	trsFloor = 0.6
	// trsRefreshInterval bounds ledger re-reads; the ledger only grows by a
	// few lines per turn, so minutes-stale factors are fine.
	trsRefreshInterval = 10 * time.Minute
)

// trsState is the cached per-page demotion factor set, rebuilt from the
// recall-usage ledger at most once per trsRefreshInterval.
type trsState struct {
	mu        sync.Mutex
	factors   map[string]float64 // only pages with factor < 1 are present
	refreshed time.Time
}

// recallTRSEnabled is the kill switch (DENEB_WIKI_TRS=0). Default ON: the
// demote-only shape plus the evidence bar make the failure mode "a noisy page
// ranks slightly lower", and the gold-set bench is the regression watch.
func recallTRSEnabled() bool {
	return strings.TrimSpace(os.Getenv("DENEB_WIKI_TRS")) != "0"
}

// trsFactorFor maps one page's window usage to its demotion factor.
func trsFactorFor(u RecallUsage) float64 {
	if u.Used() || u.Injects < trsMinExposures {
		return 1
	}
	if u.Injects >= trsSaturationExposures {
		return trsFloor
	}
	// Linear ramp between the evidence bar and saturation: each additional
	// ignored exposure is one more unit of evidence, nothing cleverer.
	span := float64(trsSaturationExposures - trsMinExposures)
	progress := float64(u.Injects-trsMinExposures) / span
	return 1 - progress*(1-trsFloor)
}

// recallTRSFactors returns the current per-page demotion map, refreshing from
// the ledger when stale. Never nil; an empty map means no page qualifies.
func (s *Store) recallTRSFactors(now time.Time) map[string]float64 {
	s.trs.mu.Lock()
	defer s.trs.mu.Unlock()
	if s.trs.factors != nil && now.Sub(s.trs.refreshed) < trsRefreshInterval {
		return s.trs.factors
	}
	usage := s.recallUsageCounts(now.Add(-recallHitScoreWindow))
	factors := make(map[string]float64)
	for path, u := range usage {
		if f := trsFactorFor(u); f < 1 {
			factors[path] = f
		}
	}
	s.trs.factors = factors
	s.trs.refreshed = now
	return factors
}

// applyRecallTRS demotes repeatedly-ignored pages and re-sorts. Runs after
// applyValidity at both search assembly sites; skipped entirely when the kill
// switch is set or no page qualifies.
func (s *Store) applyRecallTRS(results []SearchResult) []SearchResult {
	if len(results) == 0 || !recallTRSEnabled() {
		return results
	}
	factors := s.recallTRSFactors(time.Now())
	if len(factors) == 0 {
		return results
	}
	touched := false
	for i := range results {
		if f, ok := factors[results[i].Path]; ok {
			results[i].Score *= f
			touched = true
		}
	}
	if !touched {
		return results
	}
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].Path < results[b].Path
	})
	return results
}
