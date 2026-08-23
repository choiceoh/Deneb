package genesis

// SelfCorrectionWatchTask — the Trust Inbox choke point for self-correction
// candidates (docs/research/improvement-ideas.md 4.7). Candidates enter the
// append-only ledger from many producers (the skill_lifecycle chat tool,
// evolver rejection/tool-gap mining, runtime-error mining, recurrence
// promotion); without this watch each of them waits invisibly in
// self_correction_candidates.jsonl until someone remembers to run a batch
// review. The watch fires one feed card per NEW proposed candidate so every
// producer's output reaches the operator's approve/reject surface.
//
// Semantics mirror LadderWatchTask: the first run baselines the standing
// proposed backlog without posting (no card flood for months-old candidates),
// then each run surfaces only ids the previous snapshot had not seen. A
// delivery error leaves the candidate unseen so the next run retries.

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

// selfCorrectionWatchDefaultInterval: candidates arrive at chat/evolve
// cadence (minutes to hours); an hour keeps the card near the observation
// that produced it while staying cheap.
const selfCorrectionWatchDefaultInterval = time.Hour

// SelfCorrectionWatchTask is registered production-gated (it writes shared
// genesis state and posts operator-facing cards).
type SelfCorrectionWatchTask struct {
	Tracker *Tracker
	Logger  *slog.Logger
	// OnNew surfaces one unseen proposed candidate. An error leaves the
	// candidate unseen so the next run retries.
	OnNew func(SelfCorrectionCandidateRecord) error
}

// Name identifies the task in the autonomous scheduler.
func (t *SelfCorrectionWatchTask) Name() string { return "self-correction-watch" }

// Interval honors DENEB_SELF_CORRECTION_WATCH_INTERVAL_HOURS.
func (t *SelfCorrectionWatchTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_SELF_CORRECTION_WATCH_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return selfCorrectionWatchDefaultInterval
}

// selfCorrectionWatchStatePath sits next to the tracker ledgers.
func (t *SelfCorrectionWatchTask) selfCorrectionWatchStatePath() string {
	return filepath.Join(filepath.Dir(t.Tracker.logPath), "self_correction_watch_state.json")
}

// Run surfaces unseen proposed candidates, then persists the seen-id snapshot
// (tmp+rename) so restarts never re-fire.
func (t *SelfCorrectionWatchTask) Run(_ context.Context) error {
	if t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cands, err := t.Tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 100)
	if err != nil {
		return err
	}
	prev := map[string]bool{}
	firstRun := true
	if raw, rerr := os.ReadFile(t.selfCorrectionWatchStatePath()); rerr == nil {
		firstRun = false
		_ = json.Unmarshal(raw, &prev) // corrupt snapshot = first-run semantics
	}
	next := make(map[string]bool, len(cands))
	for _, c := range cands {
		next[c.ID] = true
		if firstRun || prev[c.ID] || t.OnNew == nil {
			continue
		}
		if err := t.OnNew(c); err != nil {
			logger.Warn("self-correction-watch: card delivery failed; candidate stays pending",
				"id", c.ID, "error", err)
			delete(next, c.ID)
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		logger.Warn("self-correction-watch: snapshot marshal failed", "error", err)
		return nil
	}
	tmp := t.selfCorrectionWatchStatePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		logger.Warn("self-correction-watch: snapshot write failed", "error", err)
		return nil
	}
	if err := os.Rename(tmp, t.selfCorrectionWatchStatePath()); err != nil {
		logger.Warn("self-correction-watch: snapshot rename failed", "error", err)
	}
	return nil
}
