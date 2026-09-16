package enginelive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Transition is one change of an engine's readiness as the watcher saw it:
// the moment traffic left the engine (Down) or came back to it.
//
// This is the only record of how long the engine was gone. The engine's own
// counters cannot say — a dead process exports nothing — and the speed
// sampler only counts the intervals it could scrape. On 2026-09-16 the
// gateway journal held 22 such episodes totalling eight hours, and the panel
// built from the engine's counters showed a green dot.
type Transition struct {
	Endpoint string `json:"endpoint"`
	AtMs     int64  `json:"atMs"`
	Down     bool   `json:"down"`
	// Reason is the probe's reading on a Down transition ("connection
	// refused", "health 503 draining (handing over to …)"); on an Up one it
	// says what closed the outage when it was not a ready probe.
	Reason string   `json:"reason,omitempty"`
	Models []string `json:"models,omitempty"`
}

// ledgerRetain bounds the persisted history; the speed history keeps the same
// month, so the two views cover the same days.
const ledgerRetain = 30 * 24 * time.Hour

// Ledger persists transitions. Transitions are rare (tens on a bad day), so
// every one is flushed at once — a gateway restart in the middle of an outage
// must not lose the moment it began.
type Ledger struct {
	mu      sync.Mutex
	path    string
	entries []Transition
}

// DefaultLedgerPath is where the transition history lives
// (DENEB_STATE_DIR-aware, next to engine-speed.json).
func DefaultLedgerPath() string {
	return filepath.Join(config.ResolveStateDir(), "engine-liveness.json")
}

// NewLedger loads whatever history is on disk; a missing or unreadable file
// starts empty rather than failing — this is observation.
func NewLedger(path string) *Ledger {
	l := &Ledger{path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var p struct {
		Transitions []Transition `json:"transitions"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return l
	}
	l.entries = p.Transitions
	sort.SliceStable(l.entries, func(i, j int) bool { return l.entries[i].AtMs < l.entries[j].AtMs })
	return l
}

// Record appends one transition and persists. Safe on a nil Ledger.
func (l *Ledger) Record(t Transition) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries = append(l.entries, t)
	l.pruneLocked(time.UnixMilli(t.AtMs))
	snapshot := append([]Transition(nil), l.entries...)
	path := l.path
	l.mu.Unlock()
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(struct {
		Transitions []Transition `json:"transitions"`
	}{snapshot}, "", "  ")
	if err != nil {
		return
	}
	_ = atomicfile.WriteFile(path, raw, &atomicfile.Options{Perm: 0o600})
}

// Last returns the most recent transition for endpoint, if any.
func (l *Ledger) Last(endpoint string) (Transition, bool) {
	if l == nil {
		return Transition{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].Endpoint == endpoint {
			return l.entries[i], true
		}
	}
	return Transition{}, false
}

// Transitions returns endpoint's transitions at or after sinceMs, oldest
// first, plus the one transition before the window when there is one — so a
// caller reconstructing outages knows the state the window opened in.
func (l *Ledger) Transitions(endpoint string, sinceMs int64) []Transition {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Transition
	var before *Transition
	for i := range l.entries {
		t := l.entries[i]
		if t.Endpoint != endpoint {
			continue
		}
		if t.AtMs < sinceMs {
			before = &t
			continue
		}
		out = append(out, t)
	}
	if before != nil {
		out = append([]Transition{*before}, out...)
	}
	return out
}

// EarliestMs is when the ledger first saw endpoint — before it, nothing is
// known about the engine's availability.
func (l *Ledger) EarliestMs(endpoint string) (int64, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.entries {
		if t.Endpoint == endpoint {
			return t.AtMs, true
		}
	}
	return 0, false
}

// pruneLocked drops entries older than the retention window, keeping the last
// entry before the cut so an outage that began long ago still has its start.
func (l *Ledger) pruneLocked(now time.Time) {
	cut := now.Add(-ledgerRetain).UnixMilli()
	keepFrom := 0
	for i, t := range l.entries {
		if t.AtMs >= cut {
			break
		}
		keepFrom = i
	}
	if keepFrom > 0 {
		l.entries = append([]Transition(nil), l.entries[keepFrom:]...)
	}
}

// Outage is one span in which the engine was refusing requests. UntilMs is 0
// while it is still going on.
type Outage struct {
	SinceMs int64  `json:"sinceMs"`
	UntilMs int64  `json:"untilMs,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Duration is the outage's length, closed at nowMs when it is ongoing.
func (o Outage) Duration(nowMs int64) time.Duration {
	end := o.UntilMs
	if end == 0 {
		end = nowMs
	}
	if end < o.SinceMs {
		return 0
	}
	return time.Duration(end-o.SinceMs) * time.Millisecond
}

// Outages folds transitions (oldest first) into spans. Consecutive Down
// entries continue one outage — a gateway restart re-records the down it
// finds, and that is the same outage, not a second one.
func Outages(transitions []Transition) []Outage {
	var out []Outage
	open := -1
	for _, t := range transitions {
		switch {
		case t.Down && open < 0:
			out = append(out, Outage{SinceMs: t.AtMs, Reason: t.Reason})
			open = len(out) - 1
		case !t.Down && open >= 0:
			out[open].UntilMs = t.AtMs
			open = -1
		}
	}
	return out
}

// DayDowntime is what one local day lost to outages.
type DayDowntime struct {
	// Seconds is the part of the day's outages that fell inside the day.
	Seconds float64
	// Episodes counts outages that BEGAN in the day, so a span across
	// midnight is one episode, on the day it started.
	Episodes int
}

// DowntimeByDay clips outages to local days in loc. An ongoing outage is
// closed at nowMs.
func DowntimeByDay(outages []Outage, nowMs int64, loc *time.Location) map[string]DayDowntime {
	if loc == nil {
		loc = time.Local
	}
	out := make(map[string]DayDowntime)
	for _, o := range outages {
		end := o.UntilMs
		if end == 0 || end > nowMs {
			end = nowMs
		}
		if end <= o.SinceMs {
			continue
		}
		start := time.UnixMilli(o.SinceMs).In(loc)
		startDay := start.Format("2006-01-02")
		d := out[startDay]
		d.Episodes++
		out[startDay] = d
		for cur := start; cur.UnixMilli() < end; {
			dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, loc)
			nextDay := dayStart.AddDate(0, 0, 1)
			segEnd := end
			if nextDay.UnixMilli() < segEnd {
				segEnd = nextDay.UnixMilli()
			}
			key := dayStart.Format("2006-01-02")
			d := out[key]
			d.Seconds += float64(segEnd-cur.UnixMilli()) / 1000
			out[key] = d
			cur = nextDay
		}
	}
	return out
}
