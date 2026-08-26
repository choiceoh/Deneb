package lanewatch

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultInterval is how often the watch reads every lane. Six hours is well
// under the shortest silence budget any lane should carry, so a real outage is
// caught within a fraction of its window while the read cost stays trivial.
const defaultInterval = 6 * time.Hour

// Task runs the watch on a schedule and reports findings. Advisory by
// construction — it never blocks or mutates a lane, it only says what it saw.
// That is deliberate: a liveness watch that can itself break a lane would be
// one more silent failure to hunt.
type Task struct {
	Watch  *Watch
	Logger *slog.Logger
	// OnFindings surfaces the findings where a person will see them (a work-feed
	// card). Optional — the log line is the floor.
	OnFindings func(findings []Finding)
}

// Name identifies the task in the autonomous scheduler.
func (t *Task) Name() string { return "lane-liveness" }

// Interval honors DENEB_LANE_WATCH_INTERVAL_HOURS as a calibration knob.
func (t *Task) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_LANE_WATCH_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return defaultInterval
}

// Run reads every lane once and reports what has gone quiet.
func (t *Task) Run(ctx context.Context) error {
	if t == nil || t.Watch == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	findings := t.Watch.Check(ctx)
	// Always log the run, even with nothing to report.
	//
	// This line started at Debug — "an all-green watch every six hours would
	// bury the one line that matters" — and that was wrong in the one way this
	// package exists to prevent: with the run invisible, a watch that never
	// FIRED looked exactly like a watch reporting all-clear. Verified 2026-08-26
	// on the first deploy: lane-liveness was registered, no Warn appeared, and
	// there was no way to tell whether it had run at all.
	//
	// One Info line every six hours is cheap. A watchdog that cannot prove it is
	// awake is not a watchdog.
	logger.Info("lane-liveness: 점검 완료",
		"lanes", t.Watch.LaneCount(), "findings", len(findings))
	if len(findings) == 0 {
		return nil
	}
	for _, f := range findings {
		// Warn, not Info. The whole point is that these were invisible; a lane
		// that stopped working is exactly the line an operator should find when
		// they grep the journal for trouble.
		logger.Warn("lane-liveness: 레인이 조용함", "lane", f.Lane, "detail", f.String())
	}
	if t.OnFindings != nil {
		t.OnFindings(findings)
	}
	return nil
}
