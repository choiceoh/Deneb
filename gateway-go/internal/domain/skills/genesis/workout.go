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
//   - Only failures are recorded; a passing workout carries no actionable
//     evidence and would just bloat the sidecar.
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

func (t *SkillWorkoutTask) Name() string            { return "skill-workout" }
func (t *SkillWorkoutTask) Interval() time.Duration { return skillWorkoutInterval }

func (t *SkillWorkoutTask) Run(ctx context.Context) error {
	if t == nil || t.Tracker == nil || t.Catalog == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	replay := t.replay
	if replay == nil {
		if t.Engine == nil {
			return nil
		}
		executor, model := t.Engine.executorSnapshot()
		if executor == nil {
			return nil // no local replay model wired → lane idles for free
		}
		replay = func(ctx context.Context, skillBody string, tc SkillValidationCaseRecord) (skillReplayTrace, error) {
			return t.Engine.runReplayExecutorWith(ctx, executor, model, skillBody, tc.Replay)
		}
	}

	entries := t.Catalog.List()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Skill.Name < entries[j].Skill.Name })
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

		for _, tc := range evaluable {
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
			errMsg := fmt.Sprintf("workout replay failed %d/%d assertions on case %s: %s",
				score.Total-score.Passed, score.Total, validationCaseLabel(tc), formatValidationFailures(score.Failures))
			if rerr := t.Tracker.RecordUsage(UsageRecord{
				SkillName:  name,
				SessionKey: workoutSessionPrefix + strconv.FormatInt(cycle, 10),
				Success:    false,
				ErrorMsg:   truncateRunes(errMsg, 500),
				Source:     UsageSourceWorkout,
			}); rerr != nil {
				logger.Warn("skill-workout: usage record failed", "skill", name, "error", rerr)
			}
		}
	}
	logger.Info("skill-workout: cycle complete", "skillsExercised", exercised, "failuresRecorded", failures)
	return nil
}
