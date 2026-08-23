// dreamer_synthesis_index.go — the synthesis prompt's capped index view.
//
// The synthesis prompt used to inject the FULL index (SnapshotIndex().Render())
// every cycle. The wiki grows past a few hundred pages (live 2026-07: 834), so
// the index came to dwarf the diary content the dreamer is actually digesting —
// diluting the signal, burning tokens, and risking a truncated rules/diary tail
// on the output budget. This file selects the routing-relevant subset and
// renders it in the same TSV format the model already knows.
//
// The critique pass keeps the FULL index: duplicate detection genuinely needs
// breadth. Synthesis needs routing targets — anchors that actually get
// recalled, core high-importance pages, and recently-active pages — not every
// row. Near-duplicate creates the subset misses are still converged downstream
// by findExistingPage and the verify duplicate pass.
package wiki

import (
	"sort"
	"time"
)

const (
	// synthesisIndexEntryLimit bounds how many index rows the synthesis prompt
	// carries.
	synthesisIndexEntryLimit = 120
	// synthesisIndexRecentDays: pages updated within this window are "active"
	// and stay in the synthesis view even at low importance.
	synthesisIndexRecentDays = 14
	// synthesisIndexAnchorScore is the rank bonus an actually-recalled page
	// gets, guaranteeing it lands in the capped view.
	synthesisIndexAnchorScore = 1.0
	// synthesisIndexRecentScore is the rank bonus a recently-updated page gets.
	synthesisIndexRecentScore = 0.3
)

// selectSynthesisIndexEntries picks the routing-relevant subset of the index:
// anchors (pages actually recalled into turns), plus high-importance and
// recently-updated pages, capped at synthesisIndexEntryLimit. Deterministic —
// ties break by path — and pure, so it is unit-testable without a store.
func selectSynthesisIndexEntries(entries map[string]IndexEntry, anchors map[string]struct{}, now time.Time) map[string]IndexEntry {
	recentCutoff := now.AddDate(0, 0, -synthesisIndexRecentDays).Format("2006-01-02")
	type candidate struct {
		path  string
		entry IndexEntry
		score float64
	}
	cands := make([]candidate, 0, len(entries))
	for path, e := range entries {
		score := e.Importance
		if _, ok := anchors[path]; ok {
			score += synthesisIndexAnchorScore
		}
		if e.Updated != "" && e.Updated >= recentCutoff {
			score += synthesisIndexRecentScore
		}
		cands = append(cands, candidate{path: path, entry: e, score: score})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].path < cands[j].path
	})
	if len(cands) > synthesisIndexEntryLimit {
		cands = cands[:synthesisIndexEntryLimit]
	}
	out := make(map[string]IndexEntry, len(cands))
	for _, c := range cands {
		out[c.path] = c.entry
	}
	return out
}

// currentOrdinaryIndexEntries resolves the metadata-only master index back to
// pages before it enters an LLM prompt. This keeps history and the generated
// current-fact compatibility view out of both synthesis and critique without
// deleting them from the audit-oriented master index.
func (wd *WikiDreamer) currentOrdinaryIndexEntries(entries map[string]IndexEntry) map[string]IndexEntry {
	if wd == nil || wd.store == nil || len(entries) == 0 {
		return nil
	}
	out := make(map[string]IndexEntry, len(entries))
	for path, entry := range entries {
		page, err := wd.store.ReadPage(path)
		if err != nil || !isCurrentOrdinaryPage(path, page) {
			continue
		}
		out[path] = entry
	}
	return out
}
