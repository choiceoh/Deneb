package genesis

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
)

// DreamToSkillTask is a periodic background task that scans Aurora summaries
// for skill-worthy patterns. It bridges the dreaming system (fact extraction)
// with skill genesis (workflow extraction).
//
// While dreaming extracts facts for memory, this task extracts reusable
// procedures for the skill catalog — different outputs from the same source.
type DreamToSkillTask struct {
	genesis     *Service
	auroraStore *aurora.Store
	logger      *slog.Logger

	// processedIDs tracks which summaries have been evaluated to avoid re-processing.
	processedIDs map[string]bool
}

// NewDreamToSkillTask creates a new dream-to-skill bridge task.
func NewDreamToSkillTask(genesis *Service, auroraStore *aurora.Store, logger *slog.Logger) *DreamToSkillTask {
	if logger == nil {
		logger = slog.Default()
	}
	return &DreamToSkillTask{
		genesis:      genesis,
		auroraStore:  auroraStore,
		logger:       logger,
		processedIDs: make(map[string]bool),
	}
}

// Name returns the task identifier.
func (t *DreamToSkillTask) Name() string { return "dream-to-skill" }

// Interval returns how often to scan for skill-worthy summaries.
func (t *DreamToSkillTask) Interval() time.Duration { return 4 * time.Hour }

// Run scans recent Aurora condensed summaries for skill-worthy patterns.
func (t *DreamToSkillTask) Run(ctx context.Context) error {
	if t.auroraStore == nil || t.genesis == nil {
		return nil
	}

	// Fetch condensed summaries (depth >= 1).
	summaries, err := t.auroraStore.FetchRecentSummaries(10)
	if err != nil {
		return err
	}

	processed := 0
	generated := 0
	for _, summary := range summaries {
		if ctx.Err() != nil {
			break
		}

		// Skip already-processed summaries.
		if t.processedIDs[summary.SummaryID] {
			continue
		}

		// Need enough content to extract patterns.
		if summary.TokenCount < 200 || summary.Content == "" {
			t.processedIDs[summary.SummaryID] = true
			continue
		}

		skill, err := t.genesis.GenerateFromDream(ctx, summary.Content)
		if err != nil {
			t.logger.Debug("dream-to-skill: generation failed",
				"summaryId", summary.SummaryID, "error", err)
			t.processedIDs[summary.SummaryID] = true
			processed++
			continue
		}

		if skill != nil {
			if err := t.genesis.Persist(skill); err != nil {
				t.logger.Warn("dream-to-skill: persist failed",
					"skill", skill.Name, "error", err)
			} else {
				t.logger.Info("dream-to-skill: new skill from dream",
					"skill", skill.Name,
					"summaryId", summary.SummaryID,
				)
				generated++
			}
		}

		t.processedIDs[summary.SummaryID] = true
		processed++
	}

	// Prune old entries to prevent memory growth.
	if len(t.processedIDs) > 500 {
		t.processedIDs = make(map[string]bool)
	}

	if processed > 0 {
		t.logger.Debug("dream-to-skill: cycle complete",
			"processed", processed, "generated", generated)
	}
	return nil
}
