// dreamer_quality_ledger.go — persist the per-cycle dream quality score.
//
// computeDreamQuality already grades each cycle's output along three axes
// (precision, confidence, and the recall utility of pages the DREAMER itself
// wrote). Before this ledger that number was logged and put on a workfeed card
// and then discarded — the slow loop that rewrites the synthesis rules
// (dreamer_selfcompare.go) could not see whether a rules revision actually
// moved the dreamer's output quality, only whether the pairwise style judge
// kept liking it. This append-only sidecar (a sibling of
// .dream-selfcompare.jsonl and .recall-hits.jsonl) makes the score — and, via
// RulesHash, the rules version it was produced under — durable so the trend is
// available to the revision prompt and, later, to any operator audit.
//
// The ledger is advisory by construction, like every other dreamer sidecar: an
// append failure surfaces as a phase error (visible in the dream notification)
// but never fails the cycle.
package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// dreamQualityFile is the append-only quality ledger beside the other
	// dreamer state dotfiles in the wiki dir. Dot prefix + .jsonl suffix keep
	// it out of the .md page index.
	dreamQualityFile = ".dream-quality.jsonl"
	// dreamQualityLedgerMaxBytes triggers tail-rotation of the ledger. At ~3
	// cycles/day a 256KB budget holds months of entries; rotation is a backstop
	// against a pathological hot-loop, not a regular event.
	dreamQualityLedgerMaxBytes = 256 * 1024
	// dreamQualityLedgerKeep is the tail kept after rotation.
	dreamQualityLedgerKeep = 512
)

// dreamQualityEntry is one ledger line: a cycle's decomposed quality score and
// the rules version it was produced under. Utility carries the recall rate of
// pages the dreamer wrote in earlier cycles; UtilitySet distinguishes "the axis
// had no evidence this cycle" (false) from "the axis read 0.0 because every
// page was cold" (true) — 0.0 is a real reading, not a missing one.
//
// Precision and Confidence carry no omitempty for that same reason, and they
// used to. A cycle that proposed one update and applied none scores precision
// 0.0, which omitempty erased — so the line read as "this axis was not
// measured" when it in fact recorded a total miss. Three of the four lowest
// scores in the live ledger looked like unmeasured cycles until the scoring
// code was read (analysis, 2026-08-30); an axis worth logging is worth
// distinguishing from silence.
type dreamQualityEntry struct {
	Ts            int64   `json:"ts"` // unix millis
	Score         float64 `json:"score"`
	Precision     float64 `json:"precision"`
	Confidence    float64 `json:"confidence"`
	Utility       float64 `json:"utility,omitempty"`
	UtilitySet    bool    `json:"utilitySet,omitempty"`
	RecalledPages int     `json:"recalledPages,omitempty"`
	Proposed      int     `json:"proposed,omitempty"`
	Applied       int     `json:"applied,omitempty"`
	RulesHash     string  `json:"rulesHash,omitempty"`
}

// dreamQualityPath returns the ledger path inside the wiki dir.
func (wd *WikiDreamer) dreamQualityPath() string {
	return filepath.Join(wd.store.Dir(), dreamQualityFile)
}

// appendDreamQualityEntry appends one JSONL line and tail-rotates the ledger
// when it outgrows its byte budget.
func (wd *WikiDreamer) appendDreamQualityEntry(entry dreamQualityEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal quality entry: %w", err)
	}
	path := wd.dreamQualityPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open quality ledger: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("append quality ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close quality ledger: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.Size() > dreamQualityLedgerMaxBytes {
		wd.rotateDreamQualityLedger(path)
	}
	return nil
}

// rotateDreamQualityLedger keeps the tail of the ledger. Best-effort: a failed
// rotation leaves the oversized-but-valid original in place.
func (wd *WikiDreamer) rotateDreamQualityLedger(path string) {
	entries := wd.readDreamQualityEntries()
	if len(entries) > dreamQualityLedgerKeep {
		entries = entries[len(entries)-dreamQualityLedgerKeep:]
	}
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// readDreamQualityEntries scans the ledger, skipping malformed lines.
func (wd *WikiDreamer) readDreamQualityEntries() []dreamQualityEntry {
	data, err := os.ReadFile(wd.dreamQualityPath())
	if err != nil {
		return nil
	}
	var out []dreamQualityEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e dreamQualityEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

// dreamQualityTrendWindow bounds how many recent utility readings the trend
// compares first-vs-last over.
const dreamQualityTrendWindow = 6

// dreamUtilityTrendThreshold is the minimum first-vs-last utility movement
// before the trend reads as 상승/하락 instead of 유지 — a per-cycle utility
// signal is noisy, and a small swing must not read as a directive.
const dreamUtilityTrendThreshold = 0.05

// renderDreamQualityTrend reads the quality ledger and renders a compact
// first-vs-last utility trend over the most recent cycles that reported a
// utility axis. It is the ledger's consumer: without a reader the persisted
// scores would be write-only — the exact "measure then discard" gap this
// ledger closes. Advisory prose only: it feeds the rules-revision prompt as a
// hint, never a gate. Returns "" when fewer than two cycles reported utility.
func (wd *WikiDreamer) renderDreamQualityTrend() string {
	entries := wd.readDreamQualityEntries()
	var useful []dreamQualityEntry
	for _, e := range entries {
		if e.UtilitySet {
			useful = append(useful, e)
		}
	}
	if len(useful) < 2 {
		return ""
	}
	if len(useful) > dreamQualityTrendWindow {
		useful = useful[len(useful)-dreamQualityTrendWindow:]
	}
	first, last := useful[0], useful[len(useful)-1]
	dir := "유지"
	switch {
	case last.Utility > first.Utility+dreamUtilityTrendThreshold:
		dir = "상승"
	case last.Utility < first.Utility-dreamUtilityTrendThreshold:
		dir = "하락"
	}
	return fmt.Sprintf("드리머 작성 페이지 회상률(utility) 추세: %.2f → %.2f (%s, %d 사이클)",
		first.Utility, last.Utility, dir, len(useful))
}
