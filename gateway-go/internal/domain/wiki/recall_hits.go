// recall_hits.go — the recall-utility ledger: which wiki pages actually
// surfaced as recall evidence in a chat turn, and which of those the model
// then observably USED.
//
// The dreamer writes pages blind — it never learned which of them earned their
// keep, so it could pile up noise pages forever with no feedback (an open loop,
// the RSI roadmap's 효용 접지 gap). This append-only sidecar closes the loop: the
// recall preflight records every wiki page it injected as evidence, and the
// dream cycle reads the aggregated counts to (1) score its own output quality,
// (2) flag never-recalled low-value pages for archival, and (3) bias synthesis
// toward the anchors that actually get used. Best-effort by contract — a ledger
// failure never blocks a chat turn or a dream cycle.
//
// Injection alone is EXPOSURE, not use — the two are near-independent in
// agentic retrieval (bridge-evidence adoption, arXiv 2607.15253). Each line
// therefore carries an event kind — inject (surfaced as recall evidence),
// read (the model opened the page via a wiki/knowledge read tool), cite (the
// final answer referenced an injected page) — so scoring can weight observed
// use above exposure. Inject lines additionally carry the retrieval context
// (query label, injection rank, preflight score) so real-traffic (query →
// page) pairs can be mined as recall-bench gold-set candidates, plus the chat
// session so usage events can be attributed against the exposure.
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

// Recall ledger event kinds. A legacy line without an event field is an
// inject — the only kind that existed before usage signals were added.
const (
	RecallEventInject = "inject" // page surfaced as recall evidence (exposure)
	RecallEventRead   = "read"   // model opened the page via a read tool (use)
	RecallEventCite   = "cite"   // final answer referenced an injected page (use)
)

// recallHitQueryMaxRunes bounds the recorded query label so a pathological cue
// cannot bloat the ledger; 200 runes keeps every typical Korean cue intact.
const recallHitQueryMaxRunes = 200

// RecallEvent is one ledger entry to record: a page path plus what happened
// to it. Query/Rank/Score are inject-side retrieval context (the query is the
// cue label — the primary search query or an anchor sentinel like
// "project-anchor" — not necessarily the exact clause that matched); Session
// attributes the event to a chat session so exposure and use can be joined.
type RecallEvent struct {
	Path    string  // wiki page rel path
	Event   string  // RecallEventInject | RecallEventRead | RecallEventCite ("" = inject)
	Query   string  // inject: the cue label that surfaced the page
	Rank    int     // inject: 1-based position in the injected evidence block
	Score   float64 // inject: preflight ranking score at injection
	Session string  // chat session key ("" when the surface has none)
	// Gate-shadow signals (inject only; arXiv 2607.14390's episodic confidence
	// gate, SHADOW phase): turn-level confidence of the WHOLE injected block —
	// Top1 is the highest ranked score, Gap = Top1 − runner-up score (0 with a
	// single row), Cue whether the user explicitly asked for recall. Recorded
	// on every inject line of the turn so the silence gate can be calibrated
	// offline against read/cite outcomes (scripts/audit/
	// recall_gate_calibration.py) BEFORE any production gating exists.
	Top1 float64
	Gap  float64
	Cue  bool
}

// recallHit is one persisted ledger line. Context fields are omitempty so
// legacy (path, at)-only lines and new lines share one schema; readers treat
// a missing event as inject and zero context values as "unknown".
type recallHit struct {
	Path    string  `json:"path"`
	At      int64   `json:"at"`                // unix milli
	Event   string  `json:"event,omitempty"`   // "" (legacy) == RecallEventInject
	Query   string  `json:"query,omitempty"`   // inject: cue label that surfaced the page
	Rank    int     `json:"rank,omitempty"`    // inject: 1-based position in the evidence block
	Score   float64 `json:"score,omitempty"`   // inject: preflight ranking score
	Session string  `json:"session,omitempty"` // chat session key
	Top1    float64 `json:"top1,omitempty"`    // inject: turn's top-1 ranked score (gate shadow)
	Gap     float64 `json:"gap,omitempty"`     // inject: top1 − runner-up score (gate shadow)
	Cue     bool    `json:"cue,omitempty"`     // inject: explicit recall cue turn (gate shadow)
}

// RecordRecallEvents appends one ledger line per event. Inject events come
// from the recall preflight after the final evidence set is ranked (each
// recorded path is a page the turn actually pulled into context); read/cite
// events come from the model-facing read tools and the end-of-turn citation
// pass. Empty paths and (path, event) duplicates within a single call are
// collapsed keeping the first (best-ranked) record. Returns an error only for
// the caller to log — recall utility telemetry is best-effort and must never
// fail a chat turn.
func (s *Store) RecordRecallEvents(events []RecallEvent) error {
	if len(events) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	type dedupKey struct{ path, event string }
	seen := make(map[dedupKey]struct{}, len(events))
	var buf []byte
	for _, ev := range events {
		path := strings.TrimSpace(ev.Path)
		if path == "" {
			continue
		}
		kind := strings.TrimSpace(ev.Event)
		if kind == "" {
			kind = RecallEventInject
		}
		key := dedupKey{path, kind}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		line, err := json.Marshal(recallHit{
			Path:  path,
			At:    now,
			Event: kind,
			Query: clipRunes(strings.TrimSpace(ev.Query), recallHitQueryMaxRunes),
			Rank:  ev.Rank,
			// Round so a derived telemetry line doesn't carry 17-digit float noise.
			Score:   math.Round(ev.Score*1e4) / 1e4,
			Session: strings.TrimSpace(ev.Session),
			Top1:    math.Round(ev.Top1*1e4) / 1e4,
			Gap:     math.Round(ev.Gap*1e4) / 1e4,
			Cue:     ev.Cue,
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
		if h.Event == "" { // legacy pre-event line: injection was all we recorded
			h.Event = RecallEventInject
		}
		out = append(out, h)
	}
	return out
}

// verificationSessionPrefixes is the single authority for chat-session lanes
// whose ledger events are VERIFICATION traffic, not demand: live-test chats,
// the Korean recall probe, and puppet-seat measurements all exercise recall
// mechanically, and counting their injects/reads as usage inflates exactly
// the utility signal they exist to measure. Autonomous production lanes
// (cron, heartbeat, mail analysis) stay counted — they act on the operator's
// behalf, and their use of a page is real demand. Legacy lines with no
// session also stay counted: an unclassifiable line must not zero history.
var verificationSessionPrefixes = []string{
	"client:lt-", "client:korean-probe", "client:puppet-",
}

func isVerificationSession(session string) bool {
	for _, p := range verificationSessionPrefixes {
		if strings.HasPrefix(session, p) {
			return true
		}
	}
	return false
}

// recallHitCounts aggregates events at or after `since` into per-path counts.
// Every event kind counts: a page's count is total ledger activity, so pages
// with observed use (read/cite on top of inject) naturally weigh heavier in
// the synthesis anchor list. Kind-split aggregation is recallUsageCounts.
func (s *Store) recallHitCounts(since time.Time) map[string]int {
	cutoff := since.UnixMilli()
	s.recallMu.Lock()
	hits := s.readRecallHitsLocked()
	s.recallMu.Unlock()
	counts := make(map[string]int)
	for _, h := range hits {
		if h.At < cutoff || isVerificationSession(h.Session) {
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

// RecallUsage splits one page's ledger events by kind over a window. Injects
// is exposure (recall pulled the page into context); Reads and Cites are
// observed use (the model opened the page / the final answer referenced it).
// The split exists because exposure does not predict use (bridge-evidence
// adoption) — consumers must not equate the two.
type RecallUsage struct {
	Injects int
	Reads   int
	Cites   int
}

// Used reports whether the page earned any observed use beyond injection.
func (u RecallUsage) Used() bool { return u.Reads > 0 || u.Cites > 0 }

// recallUsageCounts aggregates events at or after `since` into per-path
// kind-split usage. An empty map when the window is empty.
func (s *Store) recallUsageCounts(since time.Time) map[string]RecallUsage {
	cutoff := since.UnixMilli()
	s.recallMu.Lock()
	hits := s.readRecallHitsLocked()
	s.recallMu.Unlock()
	usage := make(map[string]RecallUsage)
	for _, h := range hits {
		if h.At < cutoff || isVerificationSession(h.Session) {
			continue
		}
		u := usage[h.Path]
		switch h.Event {
		case RecallEventRead:
			u.Reads++
		case RecallEventCite:
			u.Cites++
		default:
			u.Injects++
		}
		usage[h.Path] = u
	}
	return usage
}

// RecallUsageScoreCounts is recallUsageCounts over the standard score window,
// stamped against now — the dream cycle's usage-grounded counterpart of
// RecallHitScoreCounts.
func (s *Store) RecallUsageScoreCounts(now time.Time) map[string]RecallUsage {
	return s.recallUsageCounts(now.Add(-recallHitScoreWindow))
}

// compactRecallHits rewrites the ledger keeping only hits at or after
// now-recallHitRetention, bounding growth. Called once per dream cycle under a
// single lock so a concurrent RecordRecallEvents cannot interleave. A no-op
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
