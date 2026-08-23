// dreamer_cadence.go — the demand-aware dream cadence signal.
//
// The dreamer fires on a fixed turn count (50) or time interval (8h), plus a
// preference fast path. What it lacked until now is the DEMAND half of the
// recall ledger: a fresh unanswered cue turn (recall_misses.go) is a measured
// hole in the curated memory, and waiting out the full batch window before the
// next consolidation defers closing it. This file exposes the freshness signal
// so ShouldDream can shorten the wait to the same min-interval floor the
// preference path already uses.
//
// Best-effort by contract: reading the demand ledger never fails the dream —
// an unreadable/missing ledger simply reads as "no fresh demand" and the
// regular thresholds still own firing.
package wiki

import "time"

// demandFreshCacheTTL bounds how often hasFreshDemand re-reads the demand
// ledger. ShouldDream runs on every chat turn; the signal only needs to be
// roughly current to shorten an 8h cadence, so a short TTL keeps a chatty
// session to one ledger read per TTL instead of one per turn.
const demandFreshCacheTTL = 5 * time.Minute

// HasRecentRecallMiss reports whether any unanswered cue turn was recorded
// within `within` of now — the freshness signal behind the demand fast path.
func (s *Store) HasRecentRecallMiss(now time.Time, within time.Duration) bool {
	cutoff := now.Add(-within).UnixMilli()
	s.recallMu.Lock()
	misses := s.readRecallMissesLocked()
	s.recallMu.Unlock()
	for _, m := range misses {
		if m.At >= cutoff {
			return true
		}
	}
	return false
}
