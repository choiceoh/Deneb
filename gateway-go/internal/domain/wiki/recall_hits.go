// recall_hits.go — the recall-utility ledger: which wiki pages actually
// surfaced as recall evidence in a chat turn.
//
// The dreamer writes pages blind — it never learned which of them earned their
// keep, so it could pile up noise pages forever with no feedback (an open loop,
// the RSI roadmap's 효용 접지 gap). This append-only sidecar closes the loop: the
// recall preflight records every wiki page it injected as evidence, and the
// dream cycle reads the aggregated counts to (1) score its own output quality,
// (2) flag never-recalled low-value pages for archival, and (3) bias synthesis
// toward the anchors that actually get used. Since 2026-07 each line also
// carries the retrieval context (query label, injection rank, preflight score)
// so real-traffic (query → page) pairs can be mined as recall-bench gold-set
// candidates and later graded for usefulness — injection alone is popularity,
// not utility. Best-effort by contract — a ledger failure never blocks a chat
// turn or a dream cycle.
package wiki

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// recallHitsFile is the append-only recall-utility ledger, a sibling of the
// prose pages. The dot prefix + .jsonl suffix keep it out of the .md page index
// (same discipline as .deals.jsonl / .diary-process-state.json).
const recallHitsFile = ".recall-hits.jsonl"

// Retention/aggregation windows. The ledger is compacted to recallHitRetention
// each dream cycle so it cannot grow unbounded; the shorter score window keeps
// the quality signal responsive to recent behavior while the retained tail
// still lets detectUnrecalled judge a page "cold" over a longer horizon.
const (
	recallHitRetention    = 90 * 24 * time.Hour
	recallHitScoreWindow  = 30 * 24 * time.Hour
	unrecalledColdMinDays = 60 // a page older than this with zero retained hits is archive-cold
)

// recallHitQueryMaxRunes bounds the recorded query label so a pathological cue
// cannot bloat the ledger; 200 runes keeps every typical Korean cue intact.
const recallHitQueryMaxRunes = 200

// recallHit is one "page P surfaced as recall evidence at time T" event, plus
// the retrieval context known at injection time. Query/Rank/Score are omitempty
// so pre-context lines and new lines coexist in one ledger; readers treat zero
// values as "unknown" (legacy line).
type recallHit struct {
	Path  string  `json:"path"`
	At    int64   `json:"at"`              // unix milli
	Query string  `json:"query,omitempty"` // recall cue label that surfaced the page
	Rank  int     `json:"rank,omitempty"`  // 1-based position in the injected evidence block
	Score float64 `json:"score,omitempty"` // preflight ranking score at injection
}

// RecallHitRecord is one injected wiki page with the retrieval context the
// recall preflight knew at injection time. Query is the cue label (the primary
// search query, or an anchor sentinel like "project-anchor") — not necessarily
// the exact clause that matched; it exists so real-traffic (query → page) pairs
// can be mined as gold-set candidates and later graded for usefulness.
type RecallHitRecord struct {
	Path  string
	Query string
	Rank  int
	Score float64
}

// RecordRecallHits appends one hit line per injected wiki page. Called from
// the recall preflight after the final evidence set is ranked, so each recorded
// path is a page the turn actually pulled into context. Empty and duplicate
// paths within a single call are collapsed keeping the first (best-ranked)
// record — one turn surfacing a page twice is one utility event. Returns an
// error only for the caller to log — recall utility telemetry is best-effort
// and must never fail a chat turn.
func (s *Store) RecordRecallHits(hits []RecallHitRecord) error {
	if len(hits) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	seen := make(map[string]struct{}, len(hits))
	var buf []byte
	for _, h := range hits {
		p := strings.TrimSpace(h.Path)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		line, err := json.Marshal(recallHit{
			Path:  p,
			At:    now,
			Query: clipRunes(strings.TrimSpace(h.Query), recallHitQueryMaxRunes),
			Rank:  h.Rank,
			// Round so a derived telemetry line doesn't carry 17-digit float noise.
			Score: math.Round(h.Score*1e4) / 1e4,
		})
		if err != nil {
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if len(buf) == 0 {
		return nil
	}
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, recallHitsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(buf)
	return err
}

// clipRunes returns s truncated to at most n runes.
func clipRunes(s string, n int) string {
	if len(s) <= n { // bytes ≤ n implies runes ≤ n
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// readRecallHitsLocked returns every retained hit line, oldest first. Caller
// MUST hold recallMu. A missing ledger yields nil (no recall recorded yet);
// malformed lines are skipped — the ledger is derived, best-effort data.
func (s *Store) readRecallHitsLocked() []recallHit {
	f, err := os.Open(filepath.Join(s.dir, recallHitsFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []recallHit
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h recallHit
		if err := json.Unmarshal([]byte(line), &h); err != nil || h.Path == "" {
			continue
		}
		out = append(out, h)
	}
	return out
}

// recallHitCounts aggregates hits at or after `since` into per-path counts. Used
// by the dream cycle for the utility-coverage quality subscore and the synthesis
// anchor list. An empty map when nothing was recalled in the window.
func (s *Store) recallHitCounts(since time.Time) map[string]int {
	cutoff := since.UnixMilli()
	s.recallMu.Lock()
	hits := s.readRecallHitsLocked()
	s.recallMu.Unlock()
	counts := make(map[string]int)
	for _, h := range hits {
		if h.At < cutoff {
			continue
		}
		counts[h.Path]++
	}
	return counts
}

// RecallHitScoreCounts is recallHitCounts over the standard score window,
// stamped against now. The dreamer passes its cycle clock so tests stay
// deterministic.
func (s *Store) RecallHitScoreCounts(now time.Time) map[string]int {
	return s.recallHitCounts(now.Add(-recallHitScoreWindow))
}

// compactRecallHits rewrites the ledger keeping only hits at or after
// now-recallHitRetention, bounding growth. Called once per dream cycle under a
// single lock so a concurrent RecordRecallHits cannot interleave. A no-op
// (dropped 0) when nothing aged out or the ledger is missing. Best-effort: an
// I/O failure leaves the ledger intact and is returned for logging.
func (s *Store) compactRecallHits(now time.Time) (dropped int, err error) {
	cutoff := now.Add(-recallHitRetention).UnixMilli()
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	hits := s.readRecallHitsLocked()
	if len(hits) == 0 {
		return 0, nil
	}
	kept := hits[:0]
	for _, h := range hits {
		if h.At < cutoff {
			dropped++
			continue
		}
		kept = append(kept, h)
	}
	if dropped == 0 {
		return 0, nil
	}
	var buf []byte
	for _, h := range kept {
		line, merr := json.Marshal(h)
		if merr != nil {
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	// Atomic replace so a crash mid-rewrite cannot truncate the ledger.
	tmp := filepath.Join(s.dir, recallHitsFile+".tmp")
	if werr := os.WriteFile(tmp, buf, 0o644); werr != nil {
		return 0, werr
	}
	if rerr := os.Rename(tmp, filepath.Join(s.dir, recallHitsFile)); rerr != nil {
		return 0, rerr
	}
	return dropped, nil
}
