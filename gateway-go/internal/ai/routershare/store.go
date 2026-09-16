// Package routershare turns the wormhole router's month-cumulative usage meter
// into per-day, per-entry served counts.
//
// Why a second store: the router keeps one counter per entry per calendar
// month. That window is the wrong shape for the question the engine panel
// asks — "how much ran locally TODAY" — and it silently straddles routing
// changes: after the 2026-09-14 rename, the month's "local" total was mostly
// cloud traffic from before the rename and the real local rows were counted
// as remote because their entries no longer existed. Differencing the meter
// on a short cadence and filing the deltas by local day gives numbers that
// belong to one day and one routing table.
package routershare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// retainDays bounds the persisted history, matching the engine-speed store.
const retainDays = 30

// flushInterval is how often accumulated state reaches disk.
const flushInterval = 5 * time.Minute

// DayEntry is one router entry's served totals for one local day.
type DayEntry struct {
	Day          string `json:"day"` // YYYY-MM-DD, local
	Model        string `json:"model"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	// Polls backed the entry's day — the sample mass behind the deltas.
	Polls int `json:"polls"`
}

// DefaultStatePath is where the day history lives.
func DefaultStatePath() string {
	return filepath.Join(config.ResolveStateDir(), "router-share.json")
}

// Store accumulates meter deltas into per-day, per-entry totals.
type Store struct {
	mu         sync.Mutex
	path       string
	days       map[string]*DayEntry // day + "\x00" + model
	previous   map[string]observe.RouterModelUsage
	prevWindow string
	dirty      bool
	lastFlush  time.Time
}

type persisted struct {
	Days []DayEntry `json:"days"`
}

// NewStore loads whatever history is on disk; a missing or unreadable file
// starts empty.
func NewStore(path string) *Store {
	s := &Store{path: path, days: map[string]*DayEntry{}, previous: map[string]observe.RouterModelUsage{}}
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
		s.days[dayKey(d.Day, d.Model)] = &d
	}
	return s
}

func dayKey(day, model string) string { return day + "\x00" + model }

// Observe folds one reading of the meter into its local day.
//
// The first reading — and the first after the router's window rolled to a new
// month, or after a counter moved backwards (router state reset) — only sets
// the baseline. Every later reading adds the growth of each entry since the
// previous one. An entry absent from a reading keeps its baseline; it simply
// served nothing.
func (s *Store) Observe(at time.Time, usage observe.RouterUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := at.Format("2006-01-02")
	rebaseline := s.prevWindow != usage.Window
	for _, m := range usage.Models {
		if prev, ok := s.previous[m.Model]; ok && !rebaseline &&
			(m.Requests < prev.Requests || m.InputTokens < prev.InputTokens || m.OutputTokens < prev.OutputTokens) {
			rebaseline = true
		}
	}
	if !rebaseline {
		for _, m := range usage.Models {
			prev, ok := s.previous[m.Model]
			if !ok {
				continue // first sight of this entry: baseline only
			}
			dReq, dIn, dOut := m.Requests-prev.Requests, m.InputTokens-prev.InputTokens, m.OutputTokens-prev.OutputTokens
			if dReq == 0 && dIn == 0 && dOut == 0 {
				continue
			}
			key := dayKey(day, m.Model)
			e := s.days[key]
			if e == nil {
				e = &DayEntry{Day: day, Model: m.Model}
				s.days[key] = e
			}
			e.Requests += dReq
			e.InputTokens += dIn
			e.OutputTokens += dOut
			e.Polls++
			s.dirty = true
		}
	}
	next := make(map[string]observe.RouterModelUsage, len(usage.Models))
	for _, m := range usage.Models {
		next[m.Model] = m
	}
	s.previous = next
	s.prevWindow = usage.Window
}

// Days returns the entries of the most recent n distinct days, newest day
// first and, within a day, most requests first. n <= 0 returns all.
func (s *Store) Days(n int) []DayEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DayEntry, 0, len(s.days))
	for _, d := range s.days {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day > out[j].Day
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Model < out[j].Model
	})
	if n > 0 {
		seen, last := 0, ""
		for i, r := range out {
			if r.Day != last {
				seen++
				last = r.Day
				if seen > n {
					return out[:i]
				}
			}
		}
	}
	return out
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
	cut := now.AddDate(0, 0, -retainDays).Format("2006-01-02")
	for key, d := range s.days {
		if d.Day < cut {
			delete(s.days, key)
		}
	}
	rows := make([]DayEntry, 0, len(s.days))
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
		return rows[i].Model < rows[j].Model
	})
	raw, err := json.MarshalIndent(persisted{Days: rows}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, raw, &atomicfile.Options{Perm: 0o600})
}
