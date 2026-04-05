package localai

import "sync/atomic"

// HubStats tracks hub-level counters for observability.
// All fields are safe for concurrent access.
type HubStats struct {
	Submitted   atomic.Int64
	Completed   atomic.Int64
	Failed      atomic.Int64
	Cancelled   atomic.Int64
	Dropped     atomic.Int64 // background requests dropped due to queue overflow
	CacheHits   atomic.Int64
	CacheMisses atomic.Int64
}

// Snapshot returns a point-in-time copy for JSON serialization.
type StatsSnapshot struct {
	Submitted   int64 `json:"submitted"`
	Completed   int64 `json:"completed"`
	Failed      int64 `json:"failed"`
	Cancelled   int64 `json:"cancelled"`
	Dropped     int64 `json:"dropped"`
	CacheHits   int64 `json:"cache_hits"`
	CacheMisses int64 `json:"cache_misses"`
}

// Snapshot captures current counter values.
func (s *HubStats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		Submitted:   s.Submitted.Load(),
		Completed:   s.Completed.Load(),
		Failed:      s.Failed.Load(),
		Cancelled:   s.Cancelled.Load(),
		Dropped:     s.Dropped.Load(),
		CacheHits:   s.CacheHits.Load(),
		CacheMisses: s.CacheMisses.Load(),
	}
}
