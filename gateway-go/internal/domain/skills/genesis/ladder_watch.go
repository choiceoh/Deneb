package genesis

// LadderWatchTask surfaces graduation-ladder READY transitions as work-feed
// cards. The GRAD card (rsi_ladder.go) is a pull surface — nobody polls a
// dashboard for the moment evidence crosses a threshold. The TRANSITION into
// READY is exactly when scarce operator attention is worth spending (ANCHOR
// 2606.06114: oversight lands best at output verification), so this task
// checks the rows on a slow clock and fires once per transition.
//
// Semantics: a row fires when its state becomes READY and the persisted
// snapshot last saw it in any other state (or never saw it). A row that
// falls back below the threshold and re-earns READY fires again — that is a
// genuinely new decision moment, not a duplicate. The engine still never
// flips a lock; the card only says the decision is now available.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ladderWatchDefaultInterval keeps the watch cheap: ladder evidence moves at
// ledger cadence (hours to days), not seconds.
const ladderWatchDefaultInterval = 6 * time.Hour

// LadderWatchTask is registered production-gated (it writes shared genesis
// state and posts operator-facing cards).
type LadderWatchTask struct {
	Tracker *Tracker
	Logger  *slog.Logger
	// OnReady surfaces one row whose evidence just reached READY. Best-effort:
	// surfacing failures never affect the snapshot write.
	OnReady func(title, detail string)
}

// Name identifies the task in the autonomous scheduler.
func (t *LadderWatchTask) Name() string { return "ladder-watch" }

// Interval honors DENEB_LADDER_WATCH_INTERVAL_HOURS.
func (t *LadderWatchTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_LADDER_WATCH_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return ladderWatchDefaultInterval
}

// ladderWatchStatePath sits next to the tracker ledgers.
func (t *LadderWatchTask) ladderWatchStatePath() string {
	return filepath.Join(filepath.Dir(t.Tracker.logPath), "ladder_watch_state.json")
}

// Run evaluates the rows, fires OnReady for fresh READY transitions, and
// persists the full snapshot (tmp+rename) so restarts never re-fire.
func (t *LadderWatchTask) Run(_ context.Context) error {
	if t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	prev := map[string]string{}
	if raw, err := os.ReadFile(t.ladderWatchStatePath()); err == nil {
		_ = json.Unmarshal(raw, &prev) // corrupt snapshot = first-run semantics
	}
	rows := t.Tracker.ladderRows()
	next := make(map[string]string, len(rows))
	for _, r := range rows {
		next[r.Title] = r.State
		if r.State != ladderStateReady || prev[r.Title] == ladderStateReady {
			continue
		}
		logger.Info("ladder-watch: row reached READY — surfacing operator decision",
			"row", r.Title, "detail", r.Detail)
		if t.OnReady != nil {
			t.OnReady(r.Title, r.Detail)
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		logger.Warn("ladder-watch: snapshot marshal failed", "error", err)
		return nil
	}
	tmp := t.ladderWatchStatePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		logger.Warn("ladder-watch: snapshot write failed", "error", err)
		return nil
	}
	if err := os.Rename(tmp, t.ladderWatchStatePath()); err != nil {
		logger.Warn("ladder-watch: snapshot rename failed", "error", err)
	}
	return nil
}
