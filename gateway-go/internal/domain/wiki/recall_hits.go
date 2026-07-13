// recall_hits.go — the recall-utility ledger: which wiki pages actually
// surfaced as recall evidence in a chat turn.
//
// The dreamer writes pages blind — it never learned which of them earned their
// keep, so it could pile up noise pages forever with no feedback (an open loop,
// the RSI roadmap's 효용 접지 gap). This append-only sidecar closes the loop: the
// recall preflight records every wiki page it injected as evidence, and the
// dream cycle reads the aggregated counts to (1) score its own output quality,
// (2) flag never-recalled low-value pages for archival, and (3) bias synthesis
// toward the anchors that actually get used. Best-effort by contract — a ledger
// failure never blocks a chat turn or a dream cycle.
package wiki

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// recallHitsFile is the append-only recall-utility ledger, a sibling of the
// prose pages. The dot prefix + .jsonl suffix keep it out of the .md page Index
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

// recallHit is one "page P surfaced as recall evidence at time T" event.
type recallHit struct {
	Path string `json:"path"`
	At   int64  `json:"at"` // unix milli
}

// RecordRecallHits appends one hit line per injected wiki page path. Called from
// the recall preflight after the final evidence set is ranked, so each recorded
// path is a page the turn actually pulled into context. Empty and duplicate
// paths within a single call are collapsed (one turn surfacing a page twice is
// one utility event). Returns an error only for the caller to log — recall
// utility telemetry is best-effort and must never fail a chat turn.
func (s *Store) RecordRecallHits(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	seen := make(map[string]struct{}, len(paths))
	var buf []byte
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		line, err := json.Marshal(recallHit{Path: p, At: now})
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

// RecallHitCounts aggregates hits at or after `since` into per-path counts. Used
// by the dream cycle for the utility-coverage quality subscore and the synthesis
// anchor list. An empty map when nothing was recalled in the window.
func (s *Store) RecallHitCounts(since time.Time) map[string]int {
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

// RecallHitScoreCounts is RecallHitCounts over the standard score window,
// stamped against now. The dreamer passes its cycle clock so tests stay
// deterministic.
func (s *Store) RecallHitScoreCounts(now time.Time) map[string]int {
	return s.RecallHitCounts(now.Add(-recallHitScoreWindow))
}

// CompactRecallHits rewrites the ledger keeping only hits at or after
// now-recallHitRetention, bounding growth. Called once per dream cycle under a
// single lock so a concurrent RecordRecallHits cannot interleave. A no-op
// (dropped 0) when nothing aged out or the ledger is missing. Best-effort: an
// I/O failure leaves the ledger intact and is returned for logging.
func (s *Store) CompactRecallHits(now time.Time) (dropped int, err error) {
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
