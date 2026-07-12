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

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

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
func (t *SkillWorkoutTask) Name() string { return "skill-workout" }

// Interval returns the component's scheduling cadence.
// Interval honors DENEB_SKILL_WORKOUT_INTERVAL_HOURS so the synthetic
// evidence lane (failure traces + validation coverage — never real usage) can
// run hotter during calibration.
func (t *SkillWorkoutTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_SKILL_WORKOUT_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return skillWorkoutInterval
}

// Run executes one scheduled task cycle.
func (t *SkillWorkoutTask) Run(ctx context.Context) error {
	if t == nil || t.Tracker == nil || t.Catalog == nil {
		return nil
	}
	logger := t.workoutLogger()
	replay, executorModel, ok := t.workoutReplay()
	if !ok {
		return nil
	}
	entries := t.Catalog.List()
	// Fair rotation: least-recently-exercised first (never-exercised leads),
	// names only as the deterministic tie-break — without this the per-cycle
	// cap would exercise the same first-N skills forever.
	lastAt, seenFailures := t.Tracker.WorkoutActivity(evolutionHealthWindow)
	sortWorkoutEntries(entries, lastAt)
	cycle := time.Now().UnixMilli()

	var exercised, failures int
	for _, entry := range entries {
		if exercised >= skillWorkoutMaxSkills {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		didExercise, recorded, stop := t.exerciseSkill(
			ctx, entry, replay, executorModel, cycle, seenFailures, logger,
		)
		if stop {
			return nil
		}
		if didExercise {
			exercised++
		}
		failures += recorded
	}
	logger.Info("skill-workout: cycle complete", "skillsExercised", exercised, "failuresRecorded", failures)
	return nil
}

func (t *SkillWorkoutTask) workoutLogger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

func (t *SkillWorkoutTask) workoutReplay() (
	func(context.Context, string, SkillValidationCaseRecord) (skillReplayTrace, error),
	string,
	bool,
) {
	if t.replay != nil {
		return t.replay, "", true
	}
	if t.Engine == nil {
		return nil, "", false
	}
	executor, model := t.Engine.executorSnapshot()
	if executor == nil {
		return nil, "", false
	}
	return func(ctx context.Context, body string, tc SkillValidationCaseRecord) (skillReplayTrace, error) {
		return t.Engine.runReplayExecutorWith(ctx, executor, model, body, tc.Replay)
	}, model, true
}

func sortWorkoutEntries(entries []skills.SkillEntry, lastAt map[string]int64) {
	sort.Slice(entries, func(i, j int) bool {
		left, right := lastAt[entries[i].Skill.Name], lastAt[entries[j].Skill.Name]
		if left != right {
			return left < right
		}
		return entries[i].Skill.Name < entries[j].Skill.Name
	})
}

func (t *SkillWorkoutTask) exerciseSkill(
	ctx context.Context,
	entry skills.SkillEntry,
	replay func(context.Context, string, SkillValidationCaseRecord) (skillReplayTrace, error),
	executorModel string,
	cycle int64,
	seenFailures map[string]map[string]bool,
	logger *slog.Logger,
) (exercised bool, recordedFailures int, stopCycle bool) {
	name := strings.TrimSpace(entry.Skill.Name)
	if name == "" || strings.TrimSpace(entry.Skill.FilePath) == "" {
		return false, 0, false
	}
	cases := t.evaluableWorkoutCases(name)
	if len(cases) == 0 {
		return false, 0, false
	}
	content, err := os.ReadFile(entry.Skill.FilePath)
	if err != nil {
		logger.Warn("skill-workout: body read failed", "skill", name, "error", err)
		return false, 0, false
	}
	body := skillBodyOnly(string(content))

	failures := 0
	for _, testCase := range cases {
		if seenFailures[name][validationCaseLabel(testCase)] {
			continue
		}
		trace, replayErr := replay(ctx, body, testCase)
		if replayErr != nil {
			logger.Warn("skill-workout: replay executor failed, ending cycle",
				"skill", name, "error", replayErr)
			return true, failures, true
		}
		if t.recordWorkoutFailure(name, executorModel, cycle, testCase, trace, logger) {
			failures++
		}
	}
	if failures == 0 {
		t.recordWorkoutRotation(name, executorModel, cycle, logger)
	}
	return true, failures, false
}

func (t *SkillWorkoutTask) evaluableWorkoutCases(name string) []SkillValidationCaseRecord {
	if window := skillEvolutionEvidenceWindow(); window > 0 {
		stats, err := t.Tracker.Stats(name)
		if err == nil && stats.LastUsed > 0 && stats.LastUsed < time.Now().Add(-window).UnixMilli() {
			return nil
		}
	}
	cases, err := t.Tracker.RecentSkillValidationCases(name, defaultSkillValidationCaseLimit)
	if err != nil {
		return nil
	}
	evaluable := make([]SkillValidationCaseRecord, 0, skillWorkoutMaxCasesPerSkill)
	for _, testCase := range cases {
		if !replayBehaviorEvaluable(testCase.Replay) {
			continue
		}
		evaluable = append(evaluable, testCase)
		if len(evaluable) >= skillWorkoutMaxCasesPerSkill {
			break
		}
	}
	return evaluable
}

func (t *SkillWorkoutTask) recordWorkoutFailure(
	name, executorModel string,
	cycle int64,
	testCase SkillValidationCaseRecord,
	trace skillReplayTrace,
	logger *slog.Logger,
) bool {
	score := scoreReplayAgainstTrace(trace, testCase)
	if score.Total == 0 || score.Passed >= score.Total {
		return false
	}
	errMsg := fmt.Sprintf("workout replay failed %d/%d assertions on case %s: %s",
		score.Total-score.Passed, score.Total, validationCaseLabel(testCase), formatValidationFailures(score.Failures))
	trimmedError := genesiscommon.TruncateRunes(errMsg, 500)
	if err := t.Tracker.RecordUsage(UsageRecord{
		SkillName: name, SessionKey: workoutSessionPrefix + strconv.FormatInt(cycle, 10),
		Model: executorModel, Success: false, ErrorMsg: trimmedError,
		FailureTrace: &UsageFailureTrace{
			Signature:      "terminal=heldout-assertion|mechanism=skill-behavior-drift",
			TerminalCause:  "held-out replay assertion failure",
			CausalStatus:   "synthetic workout replay (not real use)",
			AgentMechanism: "skill body no longer yields the proven tool plan",
			ErrorMsg:       trimmedError,
		},
		Source: UsageSourceWorkout,
	}); err != nil {
		logger.Warn("skill-workout: usage record failed", "skill", name, "error", err)
	}
	return true
}

func (t *SkillWorkoutTask) recordWorkoutRotation(name, executorModel string, cycle int64, logger *slog.Logger) {
	if err := t.Tracker.RecordUsage(UsageRecord{
		SkillName: name, SessionKey: workoutSessionPrefix + strconv.FormatInt(cycle, 10),
		Model: executorModel, Success: true, Source: UsageSourceWorkout,
	}); err != nil {
		logger.Warn("skill-workout: rotation marker record failed", "skill", name, "error", err)
	}
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
