// Package enginespeed measures how fast the fleet's own serving engines are,
// by reading the engines' /metrics rather than by timing the gateway's calls.
//
// Why the engine and not the gateway: the engine times its own work, so its
// numbers carry no network hop, no queue wait it can separate, and no
// response-header buffering. A gateway-side clock on this fleet reported a 0ms
// prefill on every turn, because the providers behind the wormhole withhold
// response headers until the first generated token — everything a caller waits
// through sat outside the measurement.
//
// What it therefore covers: the LOCAL engines, which is where six of seven
// model roles point. Cloud models expose nothing comparable and are absent from
// this view by construction.
//
// Everything is reported per local day. The engine's series are cumulative
// since it last started, so a day is the sum of the intervals inside it — which
// is also what keeps an engine restart from costing more than the one interval
// it falls in.
package enginespeed

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// retainDays bounds the persisted history. A month is long enough to see a
// serving change land and short enough that the file stays small.
const retainDays = 30

// flushInterval is how often accumulated state reaches disk. The poll cadence
// is far faster than this; persistence exists so a gateway restart does not
// erase the day so far, not to record every scrape.
const flushInterval = 5 * time.Minute

// DayStat is one engine's measured day.
type DayStat struct {
	Day      string `json:"day"` // YYYY-MM-DD, local
	Endpoint string `json:"endpoint"`
	// Model is what the engine reported serving. Endpoint-scoped: this engine
	// labels its series by engine, not by model, and serves one model.
	Model string `json:"model,omitempty"`

	// Delta is the day's accumulated growth, from which every rate is derived.
	Delta observe.EngineDelta `json:"delta"`

	// PeakConcurrency is the highest occupancy any scrape SAW. The engine
	// exports occupancy as a gauge and keeps no high-water mark of its own, so
	// this is a lower bound: a spike shorter than the poll interval is invisible
	// to it. PollIntervalSec says how coarse that bound is.
	PeakConcurrency int `json:"peakConcurrency"`
	PollIntervalSec int `json:"pollIntervalSec,omitempty"`

	// Polls backed the day; Restarts counts intervals discarded because the
	// engine's counters had reset inside them.
	Polls    int `json:"polls"`
	Restarts int `json:"restarts"`
}

// Rates is the day's throughput and occupancy.
func (d DayStat) Rates() observe.EngineRates { return d.Delta.Rates() }

// Store accumulates scrapes into per-day totals and persists them.
type Store struct {
	mu        sync.Mutex
	path      string
	days      map[string]*DayStat
	previous  map[string]observe.EngineCounters // endpoint → last scrape
	dirty     bool
	lastFlush time.Time
}

type persisted struct {
	Days []DayStat `json:"days"`
}

// NewStore loads whatever history is on disk. A missing or unreadable file
// starts an empty history rather than failing: this is observation, and losing
// it must never keep the gateway from running.
func NewStore(path string) *Store {
	s := &Store{
		path:     path,
		days:     map[string]*DayStat{},
		previous: map[string]observe.EngineCounters{},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var p persisted
	if json.Unmarshal(raw, &p) != nil {
		return s
	}
	for i := range p.Days {
		d := p.Days[i]
		s.days[dayKey(d.Day, d.Endpoint)] = &d
	}
	return s
}

func dayKey(day, endpoint string) string { return day + "\x00" + endpoint }

// Observe folds one scrape into its day.
//
// The first scrape of an endpoint only establishes a baseline — there is no
// interval yet to attribute. Later scrapes add their interval's growth, except
// where the counters went backwards (the engine restarted): that interval is
// counted as a restart and dropped, and the new reading becomes the baseline.
func (s *Store) Observe(endpoint string, at time.Time, pollInterval time.Duration, c observe.EngineCounters) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := at.Format("2006-01-02")
	key := dayKey(day, endpoint)
	stat := s.days[key]
	if stat == nil {
		stat = &DayStat{Day: day, Endpoint: endpoint, PollIntervalSec: int(pollInterval.Seconds())}
		s.days[key] = stat
	}
	if c.Model != "" {
		stat.Model = c.Model
	}
	stat.Polls++
	if n := c.Concurrency(); n > stat.PeakConcurrency {
		stat.PeakConcurrency = n
	}

	if prev, ok := s.previous[endpoint]; ok {
		if delta, ok := observe.EngineDeltaBetween(prev, c); ok {
			stat.Delta = stat.Delta.Add(delta)
		} else {
			stat.Restarts++
		}
	}
	s.previous[endpoint] = c
	s.dirty = true
}

// Days returns the most recent n days, newest first. n <= 0 returns all.
func (s *Store) Days(n int) []DayStat {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]DayStat, 0, len(s.days))
	for _, d := range s.days {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day > out[j].Day
		}
		return out[i].Endpoint < out[j].Endpoint
	})
	if n > 0 {
		out = limitToDays(out, n)
	}
	return out
}

// limitToDays keeps every row belonging to the newest n distinct days, so two
// engines measured on the same day are never split apart by a row count.
func limitToDays(rows []DayStat, n int) []DayStat {
	seen, last := 0, ""
	for i, r := range rows {
		if r.Day != last {
			seen++
			last = r.Day
			if seen > n {
				return rows[:i]
			}
		}
	}
	return rows
}

// MaybeFlush persists when there is something new and enough time has passed.
func (s *Store) MaybeFlush(now time.Time) error {
	s.mu.Lock()
	if !s.dirty || now.Sub(s.lastFlush) < flushInterval {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.Flush(now)
}

// Flush persists the retained history.
func (s *Store) Flush(now time.Time) error {
	s.mu.Lock()
	s.prune(now)
	rows := make([]DayStat, 0, len(s.days))
	for _, d := range s.days {
		rows = append(rows, *d)
	}
	s.dirty = false
	s.lastFlush = now
	path := s.path
	s.mu.Unlock()

	if path == "" {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Endpoint < rows[j].Endpoint
	})
	raw, err := json.MarshalIndent(persisted{Days: rows}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, raw, &atomicfile.Options{Perm: 0o600})
}

// prune drops days past the retention window. Caller holds the lock.
func (s *Store) prune(now time.Time) {
	cutoff := now.AddDate(0, 0, -retainDays).Format("2006-01-02")
	for key, d := range s.days {
		if d.Day < cutoff {
			delete(s.days, key)
		}
	}
}
