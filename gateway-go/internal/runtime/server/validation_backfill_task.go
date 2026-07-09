// validation_backfill_task.go — deterministic corpus-growth lane for the
// held-out validation bench.
//
// The per-use capture paths (init_genesis.go: recordValidationCaseFrom*Use)
// and the agent-invocable validation_backfill action both exist, yet coverage
// stayed thin in production — capture only fires on the exact turn a skill is
// consulted, and nothing periodic ever called the backfill. Result: the
// behavioral held-out gate (the strongest regression check the evolver has)
// stayed inert for most skills, which forced the proxy gates to stay
// conservative. This lane closes that loop deterministically: every cycle it
// finds skills with recent REAL use whose unique-case corpus is below target
// and replays their newest session transcripts into validation cases. No LLM
// calls — transcript extraction, weak-case guard, and dedup are all
// deterministic, so a re-run is idempotent and cheap.
package server

import (
	"context"
	"log/slog"
	"sort"
	"time"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

const (
	// validationBackfillInterval was 24h; 6h since the workout lane
	// (accelerator, 2026-07-09) consumes coverage as fast as it appears —
	// still deterministic and idempotent, so the extra cycles cost file IO.
	validationBackfillInterval = 6 * time.Hour
	// validationBackfillWindow mirrors the evolution health window: only skills
	// actually used recently earn bench coverage (dormant skills would only add
	// replay cost without protecting live behavior).
	validationBackfillWindow = 7 * 24 * time.Hour
	// validationBackfillSessionsPerSkill caps transcript replays per skill per
	// cycle; dedup makes later cycles pick up newer sessions.
	validationBackfillSessionsPerSkill = 3
	// validationBackfillTargetUniqueCases is the per-skill corpus floor the lane
	// grows toward. Matches the "a handful of held-out cases beats zero"
	// posture — enough for the behavioral gate to bite without turning every
	// evolve into a long replay run.
	validationBackfillTargetUniqueCases = 5
)

type validationBackfillTask struct {
	backend *skillLifecycleBackend
	logger  *slog.Logger
}

func (t *validationBackfillTask) Name() string            { return "validation-backfill" }
func (t *validationBackfillTask) Interval() time.Duration { return validationBackfillInterval }

func (t *validationBackfillTask) Run(ctx context.Context) error {
	if t.backend == nil || t.backend.tracker == nil || t.backend.transcripts == nil {
		return nil
	}
	sessions := t.backend.tracker.RecentRealUseSessionsBySkill(validationBackfillWindow, validationBackfillSessionsPerSkill)
	if len(sessions) == 0 {
		return nil
	}
	skills := make([]string, 0, len(sessions))
	for name := range sessions {
		skills = append(skills, name)
	}
	sort.Strings(skills)

	var recorded, skipped, covered, failed int
	for _, name := range skills {
		if err := ctx.Err(); err != nil {
			return err
		}
		if summary, err := t.backend.tracker.ValidationCaseSummary(name); err == nil &&
			summary.UniqueRecords >= validationBackfillTargetUniqueCases {
			covered++
			continue
		}
		res, err := t.backend.backfillSkillValidationCasesFromKeys(ctx, chattools.SkillValidationBackfillRequest{
			SkillName: name,
			Source:    "auto-backfill-lane",
		}, sessions[name], len(sessions[name]))
		if err != nil {
			failed++
			t.logger.Warn("validation-backfill: skill backfill failed", "skill", name, "error", err)
			continue
		}
		if n, ok := res["recorded"].(int); ok {
			recorded += n
		}
		if n, ok := res["skipped"].(int); ok {
			skipped += n
		}
	}
	t.logger.Info("validation-backfill: cycle complete",
		"skillsWithRecentUse", len(skills), "alreadyCovered", covered,
		"recorded", recorded, "skipped", skipped, "failed", failed)
	return nil
}
