package genesis

// Utility-based skill archival — the removal counterpart to genesis/curriculum
// (addition). The idle curator prunes only UNUSED skills; by construction it can
// never reach a skill that is actively used but harmful, because such a skill is
// never idle. This closes that gap.
//
// Honesty about "negative utility": true utility is (outcome with the skill −
// outcome without it), and the counterfactual is not observable at single-user
// volume, so we do NOT claim to measure it. We measure the one thing that is
// clean and counterfactual-free: an "unfixable underperformer" — a skill that
// evolution repeatedly tried to repair and that repeatedly REGRESSED (rollback
// thrash). Evolution only fires on underperformers, so a thrashing skill is a
// low-quality skill that cannot be brought to bar. Archiving it stops evolution
// from burning cycles on a lost cause and declutters the prompt. The archive is
// REVERSIBLE (archived state, operator un-archive), evidence-floored (a 1/1
// rollback never fires), and inherits the curator's agent-created + unpinned
// scope — an operator skill is never auto-removed.
//
// Workout failures (fails-own-validation) are recorded as corroborating evidence
// in the reason but are NOT a trigger on their own: a skill failing its own
// held-out cases is normally a signal to EVOLVE it (L1's job), not to remove it.
// Only the "we already tried to evolve and it regressed" history justifies removal.

import (
	"fmt"
	"strings"
	"time"
)

// skillUtilityLogScan bounds how much of the lifecycle log the utility pass
// reads; the window filter (createdAt) does the real trimming.
const skillUtilityLogScan = 1000

// skillUtilityCount is the per-skill repair-history tally the archive verdict
// reads. Fields come from two ledgers: evolves/rollbacks/rejects from the
// lifecycle log, workoutFailures from the synthetic exercise lane.
type skillUtilityCount struct {
	evolves         int
	rollbacks       int
	rejects         int
	workoutFailures int
}

// skillUtilityCounts tallies each skill's repair history within window. It MUST
// be called before ApplySkillCuratorTransitions takes t.mu: the queries below
// (RecentLifecycleLog, WorkoutActivity) lock t.mu themselves.
func (t *Tracker) skillUtilityCounts(now time.Time, window time.Duration) map[string]skillUtilityCount {
	out := map[string]skillUtilityCount{}
	cutoff := now.Add(-window).UnixMilli()

	if entries, err := t.RecentLifecycleLog(skillUtilityLogScan); err == nil {
		for _, e := range entries {
			if e.CreatedAt < cutoff {
				continue
			}
			name := strings.TrimSpace(e.SkillName)
			if name == "" {
				continue
			}
			c := out[name]
			switch e.Type {
			case "evolved":
				c.evolves++
			case "evolve_rolled_back":
				c.rollbacks++
			case "evolve_rejected":
				c.rejects++
			}
			out[name] = c
		}
	}

	// WorkoutActivity applies its own window internally; len(cases) is the count
	// of distinct held-out cases the skill's current body fails.
	if _, failed := t.WorkoutActivity(window); failed != nil {
		for name, cases := range failed {
			c := out[name]
			c.workoutFailures = len(cases)
			out[name] = c
		}
	}
	return out
}

// classifyUnfixableUnderperformer returns a non-empty archive reason when a
// skill's repair history shows rollback thrash above the configured floors, else
// "". Pure — the deterministic verdict, split out for testing.
func classifyUnfixableUnderperformer(c skillUtilityCount, cfg SkillCuratorConfig) string {
	// Evidence floor: enough committed evolves AND rollbacks to judge. A skill
	// with no committed evolve, or a single rollback, is unmeasured — never fired.
	if c.evolves <= 0 || c.rollbacks < cfg.UtilityMinRollbacks {
		return ""
	}
	// Rollback rate must clear the bar: many evolves with few rollbacks is a
	// skill that is IMPROVING, not thrashing. Integer form of
	// rollbacks/evolves >= rate%.
	if c.rollbacks*100 < cfg.UtilityRollbackRatePct*c.evolves {
		return ""
	}
	extra := ""
	if c.workoutFailures > 0 {
		extra = fmt.Sprintf(", %d workout failures", c.workoutFailures)
	}
	return fmt.Sprintf("unfixable underperformer: %d/%d committed evolves rolled back%s (reversible archive)",
		c.rollbacks, c.evolves, extra)
}
