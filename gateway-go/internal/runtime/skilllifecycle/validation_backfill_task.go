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
package skilllifecycle

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
)

const (
	// validationBackfillInterval was 24h; 6h since the workout lane
	// (accelerator, 2026-07-09) consumes coverage as fast as it appears —
	// still deterministic and idempotent, so the extra cycles cost file IO.
	validationBackfillInterval = 6 * time.Hour
	// validationBackfillSessionsPerSkill caps transcript replays per skill per
	// cycle; dedup makes later cycles pick up newer sessions.
	validationBackfillSessionsPerSkill = 3
	// validationBackfillTargetUniqueCases is the per-skill corpus floor the lane
	// grows toward. Matches the "a handful of held-out cases beats zero"
	// posture — enough for the behavioral gate to bite without turning every
	// evolve into a long replay run.
	validationBackfillTargetUniqueCases = 5
)

// Archive-sweep acceleration knobs (가속 노화, 2026-07-11): the deterministic
// extractor is idempotent and LLM-free, so during calibration the sweep can
// open up from "recent sessions" to the whole skill-attributable archive —
// the only hard bound is the usage log's own age (transcripts predating skill
// tracking carry no deterministic skill join and stay out of scope).
func backfillEnvInt(name string, def int) int {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func backfillWindow() time.Duration {
	return time.Duration(backfillEnvInt("DENEB_VALIDATION_BACKFILL_WINDOW_DAYS", 7)) * 24 * time.Hour
}

func backfillSessionsPerSkill() int {
	return backfillEnvInt("DENEB_VALIDATION_BACKFILL_SESSIONS_PER_SKILL", validationBackfillSessionsPerSkill)
}

func backfillTargetUniqueCases() int {
	return backfillEnvInt("DENEB_VALIDATION_BACKFILL_TARGET_CASES", validationBackfillTargetUniqueCases)
}

type validationBackfillTask struct {
	backend *skillLifecycleBackend
	logger  *slog.Logger
}

// NewValidationBackfillTask constructs the deterministic validation-corpus
// growth task.
//
//nolint:revive // constructor intentionally returns the concrete task; callers hand it straight to RegisterTask
func NewValidationBackfillTask(backend *Backend, logger *slog.Logger) *validationBackfillTask {
	return &validationBackfillTask{backend: backend, logger: logger}
}

// Name returns the component's stable scheduler name.
func (t *validationBackfillTask) Name() string { return "validation-backfill" }

// Interval returns the component's scheduling cadence.
func (t *validationBackfillTask) Interval() time.Duration { return validationBackfillInterval }

// Run executes one scheduled task cycle.
func (t *validationBackfillTask) Run(ctx context.Context) error {
	if t.backend == nil || t.backend.tracker == nil || t.backend.transcripts == nil {
		return nil
	}
	sessions := t.backend.tracker.RecentRealUseSessionsBySkill(backfillWindow(), backfillSessionsPerSkill())
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
			summary.UniqueRecords >= backfillTargetUniqueCases() {
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
		if res.Recorded != nil {
			recorded += *res.Recorded
		}
		if res.Skipped != nil {
			skipped += *res.Skipped
		}
	}
	t.logger.Info("validation-backfill: cycle complete",
		"skillsWithRecentUse", len(skills), "alreadyCovered", covered,
		"recorded", recorded, "skipped", skipped, "failed", failed)
	return nil
}
