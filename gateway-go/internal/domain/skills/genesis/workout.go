package genesis

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SkillWorkoutTask — synthetic exercise lane (self-improvement accelerator,
// 2026-07-09): instead of waiting for real usage to expose skill failures, it
// periodically replays each covered skill's own held-out cases against its
// CURRENT body with the local replay executor and records failed assertions as
// workout evidence. Real signal arrives at the pace of the operator's week;
// this lane produces the same evidence shape overnight.
//
// Discipline (Goodhart containment):
//   - Workout records are NEVER real usage: isRealUsageRecord excludes them, so
//     success-rate gates, curator stats, and the bench backfill stay clean.
//   - They surface only as their own failure-cluster kind (workout-failure) in
//     the sweep's evidence bundle — proposers see synthetic provenance.
//   - Failures carry the evidence; a passing skill writes only a lightweight
//     success-marker record so fair rotation (lastAt) advances for it too.
type SkillWorkoutTask struct {
	Engine  *SkillValidationEngine
	Tracker *Tracker
	Catalog *skills.Catalog
	Logger  *slog.Logger

	// replay overrides the engine executor in tests (nil → engine executor).
	replay func(ctx context.Context, skillBody string, tc SkillValidationCaseRecord) (skillReplayTrace, error)
}

const (
	skillWorkoutInterval         = 12 * time.Hour
	skillWorkoutMaxSkills        = 5
	skillWorkoutMaxCasesPerSkill = 3
	workoutSessionPrefix         = "system:skill-workout:"
)

// Name returns the component's stable scheduler name.
func (t *SkillWorkoutTask) Name() string            { return "skill-workout" }
// Interval returns the component's scheduling cadence.
func (t *SkillWorkoutTask) Interval() time.Duration { return skillWorkoutInterval }

// Run executes one scheduled task cycle.
func (t *SkillWorkoutTask) Run(ctx context.Context) error {
	if t == nil || t.Tracker == nil || t.Catalog == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	replay := t.replay
	executorModel := ""
	if replay == nil {
		if t.Engine == nil {
			return nil
		}
		executor, model := t.Engine.executorSnapshot()
		if executor == nil {
			return nil // no local replay model wired → lane idles for free
		}
		executorModel = model
		replay = func(ctx context.Context, skillBody string, tc SkillValidationCaseRecord) (skillReplayTrace, error) {
			return t.Engine.runReplayExecutorWith(ctx, executor, model, skillBody, tc.Replay)
		}
	}

	entries := t.Catalog.List()
	// Fair rotation: least-recently-exercised first (never-exercised leads),
	// names only as the deterministic tie-break — without this the per-cycle
	// cap would exercise the same first-N skills forever.
	lastAt, seenFailures := t.Tracker.WorkoutActivity(evolutionHealthWindow)
	sort.Slice(entries, func(i, j int) bool {
		li, lj := lastAt[entries[i].Skill.Name], lastAt[entries[j].Skill.Name]
		if li != lj {
			return li < lj
		}
		return entries[i].Skill.Name < entries[j].Skill.Name
	})
	cycle := time.Now().UnixMilli()

	var exercised, failures int
	for _, entry := range entries {
		if exercised >= skillWorkoutMaxSkills {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSpace(entry.Skill.Name)
		if name == "" || strings.TrimSpace(entry.Skill.FilePath) == "" {
			continue
		}
		// Mirror the evolver's recency gate (evolutionSuppressed): a skill with
		// real use OLDER than the evidence window can't be evolved anyway, so
		// exercising it would stockpile evidence with no consumer. Never-used
		// skills stay eligible, same exemption as the evolve path.
		if window := skillEvolutionEvidenceWindow(); window > 0 {
			if stats, serr := t.Tracker.Stats(name); serr == nil && stats.LastUsed > 0 &&
				stats.LastUsed < time.Now().Add(-window).UnixMilli() {
				continue
			}
		}
		cases, err := t.Tracker.RecentSkillValidationCases(name, defaultSkillValidationCaseLimit)
		if err != nil || len(cases) == 0 {
			continue
		}
		evaluable := make([]SkillValidationCaseRecord, 0, skillWorkoutMaxCasesPerSkill)
		for _, tc := range cases {
			if replayBehaviorEvaluable(tc.Replay) {
				evaluable = append(evaluable, tc)
				if len(evaluable) >= skillWorkoutMaxCasesPerSkill {
					break
				}
			}
		}
		if len(evaluable) == 0 {
			continue
		}
		content, err := os.ReadFile(entry.Skill.FilePath)
		if err != nil {
			logger.Warn("skill-workout: body read failed", "skill", name, "error", err)
			continue
		}
		body := skillBodyOnly(string(content))
		exercised++
		skillFailures := 0

		for _, tc := range evaluable {
			// A defect already evidenced inside the window stays one cluster
			// member — re-recording it every cycle would inflate support on a
			// single unfixed failure and drown the sweep's evidence ranking.
			if seenFailures[name][validationCaseLabel(tc)] {
				continue
			}
			trace, terr := replay(ctx, body, tc)
			if terr != nil {
				// Executor trouble is systemic (model down, timeout) — stop the
				// cycle instead of burning through every skill; next interval
				// retries. Fail-open, same doctrine as the behavioral gate.
				logger.Warn("skill-workout: replay executor failed, ending cycle",
					"skill", name, "error", terr)
				return nil
			}
			score := scoreReplayAgainstTrace(trace, tc)
			if score.Total == 0 || score.Passed >= score.Total {
				continue
			}
			failures++
			skillFailures++
			errMsg := fmt.Sprintf("workout replay failed %d/%d assertions on case %s: %s",
				score.Total-score.Passed, score.Total, validationCaseLabel(tc), formatValidationFailures(score.Failures))
			if rerr := t.Tracker.RecordUsage(UsageRecord{
				SkillName:  name,
				SessionKey: workoutSessionPrefix + strconv.FormatInt(cycle, 10),
				Model:      executorModel,
				Success:    false,
				ErrorMsg:   truncateRunes(errMsg, 500),
				// Explicit trace: a stable signature per mechanism (instead of
				// keyword-classifying the assertion text) keeps one skill's
				// workout failures in one cluster, with cases in the example.
				FailureTrace: &UsageFailureTrace{
					Signature:      "terminal=heldout-assertion|mechanism=skill-behavior-drift",
					TerminalCause:  "held-out replay assertion failure",
					CausalStatus:   "synthetic workout replay (not real use)",
					AgentMechanism: "skill body no longer yields the proven tool plan",
					ErrorMsg:       truncateRunes(errMsg, 500),
				},
				Source: UsageSourceWorkout,
			}); rerr != nil {
				logger.Warn("skill-workout: usage record failed", "skill", name, "error", rerr)
			}
		}
		// Rotation marker: a skill that passed every case leaves no failure
		// record, so without this its lastAt stays 0 and it sorts first forever —
		// starving later skills. A success-marker (Source=workout, excluded from
		// real stats) advances lastAt for every exercised skill.
		if skillFailures == 0 {
			if rerr := t.Tracker.RecordUsage(UsageRecord{
				SkillName:  name,
				SessionKey: workoutSessionPrefix + strconv.FormatInt(cycle, 10),
				Model:      executorModel,
				Success:    true,
				Source:     UsageSourceWorkout,
			}); rerr != nil {
				logger.Warn("skill-workout: rotation marker record failed", "skill", name, "error", rerr)
			}
		}
	}
	logger.Info("skill-workout: cycle complete", "skillsExercised", exercised, "failuresRecorded", failures)
	return nil
}

// WorkoutActivity summarizes the lane's recent records so a cycle can rotate
// fairly and avoid re-recording known defects: when each skill was last
// exercised, and which (skill, case-label) failures already exist inside the
// window.
func (t *Tracker) WorkoutActivity(window time.Duration) (lastAt map[string]int64, failedCases map[string]map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	lastAt = map[string]int64{}
	failedCases = map[string]map[string]bool{}
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return lastAt, failedCases
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	for _, r := range records {
		if r.Source != UsageSourceWorkout {
			continue
		}
		name := strings.TrimSpace(r.SkillName)
		if name == "" {
			continue
		}
		if r.UsedAt > lastAt[name] {
			lastAt[name] = r.UsedAt
		}
		if r.Success || r.UsedAt < cutoff {
			continue
		}
		if label := workoutCaseLabelFromError(r.ErrorMsg); label != "" {
			if failedCases[name] == nil {
				failedCases[name] = map[string]bool{}
			}
			failedCases[name][label] = true
		}
	}
	return lastAt, failedCases
}

// WorkoutActivitySummary is the operator-facing liveness of the synthetic
// exercise lane: the workout lane had no status surface (its evidence shows up
// only as workout-failure clusters), so this gives "is it running, and what is
// it finding" in one place (skill_lifecycle status).
type WorkoutActivitySummary struct {
	LastRunAt        int64 `json:"lastRunAt,omitempty"` // newest workout record (unix ms)
	SkillsExercised  int   `json:"skillsExercised"`     // distinct skills with a workout record in-window
	DistinctFailures int   `json:"distinctFailures"`    // distinct (skill, case) failures in-window
}

// WorkoutActivitySummarize rolls WorkoutActivity into the liveness summary over
// the evolution-health window.
func (t *Tracker) WorkoutActivitySummarize() WorkoutActivitySummary {
	lastAt, failed := t.WorkoutActivity(evolutionHealthWindow)
	var s WorkoutActivitySummary
	s.SkillsExercised = len(lastAt)
	for _, at := range lastAt {
		if at > s.LastRunAt {
			s.LastRunAt = at
		}
	}
	for _, cases := range failed {
		s.DistinctFailures += len(cases)
	}
	return s
}

// workoutCaseLabelFromError recovers the case label a workout failure was
// recorded for (see the errMsg format in Run). Empty when unparsable.
func workoutCaseLabelFromError(errMsg string) string {
	const marker = " on case "
	i := strings.Index(errMsg, marker)
	if i < 0 {
		return ""
	}
	rest := errMsg[i+len(marker):]
	// Split on ": " (colon-SPACE) — the label/failures delimiter in Run's
	// format string. A bare ":" truncated case IDs that embed a session key
	// (e.g. "session-client:main"), so dedup missed and re-recorded every cycle.
	if j := strings.Index(rest, ": "); j > 0 {
		return strings.TrimSpace(rest[:j])
	}
	return ""
}
