package genesis

// Labeler blind-spot audit (Blind Curator, arXiv 2607.07436 — RSI 2026H2
// addendum #1). The usage-success labeler is deterministic but LENIENT: a
// turn with no tool error marks every consulted skill successful, so a skill
// giving bad guidance through clean tool calls is systematically labeled a
// success (false-pass). Retirement (utility archive) and the rollback watch
// both inherit that bias — past a threshold it silently disables them.
//
// This audit is the cheap deterministic cross-check the paper prescribes:
// a skill whose post-evolve watch CONFIRMED an evolve inside the window while
// the synthetic workout lane simultaneously fails that skill's own held-out
// cases is a labeler false-pass suspect — real usage said "clean", the lane
// that actually replays the skill's contract said "broken". Evidence-only:
// no LLM, no synthetic writes into real stats (ground rule 3 intact); the
// count surfaces on rsi_status L1 and as ADVISORY meta-evidence.

import (
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// labelerBlindSpot is one false-pass suspect: a confirmed-clean skill that
// concurrently fails its own workout cases.
type labelerBlindSpot struct {
	Skill       string `json:"skill"`
	FailedCases int    `json:"failedCases"`
	ConfirmedAt int64  `json:"confirmedAt"`
}

// labelerBlindSpots joins in-window evolve confirmations against the workout
// lane's fails-own-validation records. Sorted worst-first (failed cases desc,
// then name for determinism).
func (t *Tracker) labelerBlindSpots(window time.Duration) []labelerBlindSpot {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	confirmedAt := map[string]int64{}
	for _, e := range entries {
		if e.Type != "evolve_confirmed" || e.CreatedAt < cutoff || e.SkillName == "" {
			continue
		}
		if e.CreatedAt > confirmedAt[e.SkillName] {
			confirmedAt[e.SkillName] = e.CreatedAt
		}
	}
	if len(confirmedAt) == 0 {
		return nil
	}
	_, failedCases := t.WorkoutActivity(window) // takes t.mu internally
	var out []labelerBlindSpot
	for skill, at := range confirmedAt {
		if cases := failedCases[skill]; len(cases) > 0 {
			out = append(out, labelerBlindSpot{Skill: skill, FailedCases: len(cases), ConfirmedAt: at})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FailedCases != out[j].FailedCases {
			return out[i].FailedCases > out[j].FailedCases
		}
		return out[i].Skill < out[j].Skill
	})
	return out
}
