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

// DayStat is one engine's measured day for one model.
type DayStat struct {
	Day      string `json:"day"` // YYYY-MM-DD, local
	Endpoint string `json:"endpoint"`
	// Model is what the engine reported serving. The engine labels its series
	// by engine, not by model, but one endpoint serves different models over a
	// day — a switch, a campaign's window — so the model is part of the row's
	// key. Before it was, 2026-09-19 read as one "qwen3.8-flash-next" day: the
	// label of the last scrape over GLM-5.3's counters for most of it.
	Model string `json:"model,omitempty"`
	// LastSeenMs is the newest scrape folded into the row: which of a day's
	// rows is the engine's current model, without asking the engine.
	LastSeenMs int64 `json:"lastSeenMs,omitempty"`

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
	diagnostics        []DiagnosticInterval
	diagnosticPrevious map[string]diagnosticPrevious
	mu                 sync.Mutex
	path               string
	days               map[string]*DayStat
	previous           map[string]observe.EngineCounters // endpoint → last scrape
	dirty              bool
	lastFlush          time.Time
}

type persisted struct {
	Days        []DayStat            `json:"days"`
	Diagnostics []DiagnosticInterval `json:"diagnostics,omitempty"`
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
		s.days[dayKey(d.Day, d.Endpoint, d.Model)] = &d
	}
	s.diagnostics = p.Diagnostics
	return s
}

func dayKey(day, endpoint, model string) string { return day + "\x00" + endpoint + "\x00" + model }

// Observe folds one scrape into its day and model.
//
// The first scrape of an endpoint only establishes a baseline — there is no
// interval yet to attribute. Later scrapes add their interval's growth, except
// where the counters went backwards (the engine restarted) or the model
// changed: that interval is counted as a restart and dropped, and the new
// reading becomes the baseline. A model change is a different process, so its
// counters started over — but a young old process can leave counters the new
// one has not yet passed, and the delta would then be one model's work filed
// under the other; the name says what the counters cannot.
func (s *Store) Observe(endpoint string, at time.Time, pollInterval time.Duration, c observe.EngineCounters) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, hasPrev := s.previous[endpoint]
	if c.Model == "" && hasPrev {
		// A door that could not name its model on this scrape (a handover
		// answers /v1/models with an empty catalog) is still the process that
		// answered the last one: its counters continue that model's series,
		// and its diagnostics are not a runtime change.
		c.Model = prev.Model
	}
	s.observeDiagnosticsLocked(endpoint, at, pollInterval, c)

	day := at.Format("2006-01-02")
	key := dayKey(day, endpoint, c.Model)
	stat := s.days[key]
	if stat == nil {
		stat = &DayStat{Day: day, Endpoint: endpoint, Model: c.Model, PollIntervalSec: int(pollInterval.Seconds())}
		s.days[key] = stat
	}
	stat.Polls++
	stat.LastSeenMs = at.UnixMilli()
	if n := c.Concurrency(); n > stat.PeakConcurrency {
		stat.PeakConcurrency = n
	}

	if hasPrev {
		switched := prev.Model != "" && c.Model != "" && prev.Model != c.Model
		if delta, ok := observe.EngineDeltaBetween(prev, c); ok && !switched {
			stat.Delta = stat.Delta.Add(delta)
		} else {
			stat.Restarts++
		}
	}
	s.previous[endpoint] = c
	s.dirty = true
}

// CurrentModel is the model an endpoint served at its newest scrape: the live
// sampler's last reading, or — before the first scrape since a gateway start —
// the persisted row seen last. Empty when the endpoint never named a model.
func (s *Store) CurrentModel(endpoint string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.previous[endpoint]; ok && prev.Model != "" {
		return prev.Model
	}
	var newest *DayStat
	for _, d := range s.days {
		if d.Endpoint != endpoint || d.Model == "" {
			continue
		}
		// Rows written before LastSeenMs existed carry 0; the later day wins.
		if newest == nil || d.LastSeenMs > newest.LastSeenMs ||
			(d.LastSeenMs == newest.LastSeenMs && d.Day > newest.Day) {
			newest = d
		}
	}
	if newest == nil {
		return ""
	}
	return newest.Model
}

// Days returns the most recent n days, newest first. n <= 0 returns all.
func (s *Store) Days(n int) []DayStat {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]DayStat, 0, len(s.days))
	for _, d := range s.days {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return rowBefore(out[i], out[j], true) })
	if n > 0 {
		out = limitToDays(out, n)
	}
	return out
}

// rowBefore orders rows by day (newest first when newestFirst), then endpoint,
// then model, so a day's rows keep one order in the file and in every reader.
func rowBefore(a, b DayStat, newestFirst bool) bool {
	if a.Day != b.Day {
		return (a.Day > b.Day) == newestFirst
	}
	if a.Endpoint != b.Endpoint {
		return a.Endpoint < b.Endpoint
	}
	return a.Model < b.Model
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
	s.pruneDiagnosticsLocked(now)
	diagnostics := copyDiagnosticIntervals(s.diagnostics)
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
	sort.Slice(rows, func(i, j int) bool { return rowBefore(rows[i], rows[j], false) })
	raw, err := json.MarshalIndent(persisted{Days: rows, Diagnostics: diagnostics}, "", "  ")
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
