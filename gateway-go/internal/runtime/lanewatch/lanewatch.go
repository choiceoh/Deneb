// Package lanewatch answers one question about every autonomous lane in the
// gateway: did it actually do anything recently?
//
// Deneb is built to never break a user turn. Grep the tree and that shows:
// ~326 "silently", ~295 "best-effort", ~104 "advisory", ~50 "fail-open". The
// judgement is right — a broken subsystem must not take the chat down — but it
// has one cost, and every incident in the 2026-08-26 runtime review was that
// cost coming due: a lane stopped working, nothing errored, /health stayed
// green, and the silence was indistinguishable from having nothing to do.
//
//	현장 지도 read path   the client dropped the map; the gateway kept the
//	                     reader for 10 days with only tests calling it
//	kb-interview         tombstoned 07-21; on 08-18 the operator typed two
//	                     exact triggers and nothing fired
//	evolution-proposal   the loop's own proposal skill, suppressed without
//	                     intent, found only by reading a startup log line
//	skill evolve         evolved=0 — a bad validation case had disabled the
//	                     behavioral gate, and zero looks like "nothing to do"
//	meta-evolution       a proposal sat pending from 08-03 until it expired
//
// The fix is not more error handling. It is asking, on a schedule, whether each
// lane produced work — and saying so out loud when the answer is no for longer
// than that lane should ever be quiet.
//
// The hard part is that some lanes are LEGITIMATELY quiet: a lane with no input
// has nothing to report and must not cry wolf. So a lane declares its own
// silence budget AND whether zero is a normal reading, and the watch reports
// only silence that exceeds what the lane itself says is normal. A watch that
// fires on healthy idleness gets muted, and then it is worth nothing.
package lanewatch

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Reading is what a lane reports about its own recent work.
type Reading struct {
	// Worked is the number of units of work observed in the window (cycles,
	// pages written, verdicts recorded — whatever that lane produces).
	Worked int
	// Idle marks a reading of zero that is EXPECTED right now: the lane had no
	// input to act on. A lane that cannot tell the difference leaves this false
	// and accepts the report — a false alarm is recoverable, a missed outage is
	// what this package exists to prevent.
	Idle bool
	// Detail is one short line shown with a finding, e.g. "pending since 08-03".
	Detail string
}

// Lane is one watched subsystem.
type Lane struct {
	// Name is the lane's identifier, used in logs and findings.
	Name string
	// MaxSilence is how long this lane may report no work before it is a
	// finding. Set it from the lane's own cadence with slack — a weekly lane
	// needs more than 7 days, or a single skipped cycle reads as an outage.
	MaxSilence time.Duration
	// Read returns the lane's current reading. An error is itself a finding:
	// a lane whose liveness cannot be read is not a lane known to be healthy.
	Read func(context.Context) (Reading, error)
}

// Finding is one lane that has been quiet too long, or could not be read.
type Finding struct {
	Lane   string
	Silent time.Duration
	Detail string
	Err    error
}

func (f Finding) String() string {
	if f.Err != nil {
		return fmt.Sprintf("%s: 상태를 읽을 수 없음 (%v)", f.Lane, f.Err)
	}
	line := fmt.Sprintf("%s: %s 동안 한 일이 없음", f.Lane, humanDuration(f.Silent))
	if strings.TrimSpace(f.Detail) != "" {
		line += " — " + f.Detail
	}
	return line
}

// Watch tracks when each lane last reported work and turns prolonged silence
// into findings. Safe for a single scheduler goroutine; not concurrent.
type Watch struct {
	lanes    []Lane
	lastWork map[string]time.Time
	now      func() time.Time
	logger   *slog.Logger
}

// New builds a watch over lanes. now defaults to time.Now.
func New(logger *slog.Logger, now func() time.Time, lanes ...Lane) *Watch {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &Watch{lanes: lanes, lastWork: make(map[string]time.Time, len(lanes)), now: now, logger: logger}
	// Seed every lane at construction. Without this the first check would see a
	// zero time and report every lane as silent since the epoch — the watch's
	// own startup would be its loudest false alarm.
	start := now()
	for _, lane := range lanes {
		w.lastWork[lane.Name] = start
	}
	return w
}

// Check reads every lane once and returns the findings, worst first.
//
// A lane that reports work resets its clock. A lane that reports an EXPECTED
// zero also resets it: idleness the lane vouches for is not silence, and
// counting it would make a lane that is merely unused indistinguishable from
// one that is broken — the exact confusion this package exists to remove.
func (w *Watch) Check(ctx context.Context) []Finding {
	now := w.now()
	var findings []Finding
	for _, lane := range w.lanes {
		if ctx.Err() != nil {
			return findings
		}
		reading, err := lane.Read(ctx)
		if err != nil {
			findings = append(findings, Finding{Lane: lane.Name, Err: err})
			continue
		}
		if reading.Worked > 0 || reading.Idle {
			w.lastWork[lane.Name] = now
			continue
		}
		silent := now.Sub(w.lastWork[lane.Name])
		if silent < lane.MaxSilence {
			continue
		}
		findings = append(findings, Finding{Lane: lane.Name, Silent: silent, Detail: reading.Detail})
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Silent > findings[j].Silent })
	return findings
}

// humanDuration renders a silence span the way an operator reads it.
func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d일", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d시간", int(d.Hours()))
	default:
		return fmt.Sprintf("%d분", int(d.Minutes()))
	}
}
