// dreaming_adapter.go — Implements autonomous.Dreamer for AuroraDream memory consolidation.
// Bridges the memory package's dreaming cycle with the autonomous service's lifecycle.
package memory

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const metaTurnCount = "dreaming_turn_count"
const metaLastDreaming = "dreaming_last_run"

// DreamingAdapter implements autonomous.Dreamer by wrapping the memory store's
// dreaming cycle. The autonomous service owns scheduling and event emission.
type DreamingAdapter struct {
	store    *Store
	embedder *Embedder
	client   *llm.Client
	model    string
	logger   *slog.Logger
}

// NewDreamingAdapter creates a new adapter bridging memory dreaming to autonomous.
func NewDreamingAdapter(store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) *DreamingAdapter {
	return &DreamingAdapter{
		store:    store,
		embedder: embedder,
		client:   client,
		model:    model,
		logger:   logger,
	}
}

// IncrementTurn records a conversation turn for threshold tracking.
func (da *DreamingAdapter) IncrementTurn(ctx context.Context) {
	countStr, err := da.store.GetMeta(ctx, metaTurnCount)
	if err != nil {
		da.logger.Warn("aurora-dream: failed to read turn count", "error", err)
		return
	}
	count, _ := strconv.Atoi(countStr)
	count++
	if err := da.store.SetMeta(ctx, metaTurnCount, strconv.Itoa(count)); err != nil {
		da.logger.Warn("aurora-dream: failed to write turn count", "error", err)
	}
}

// ShouldDream checks if dreaming conditions are met:
// turn count >= 50, time >= 8 hours, or active facts >= data threshold.
func (da *DreamingAdapter) ShouldDream(ctx context.Context) bool {
	// Check turn threshold.
	countStr, err := da.store.GetMeta(ctx, metaTurnCount)
	if err != nil {
		da.logger.Warn("aurora-dream: failed to read turn count for ShouldDream", "error", err)
		return false
	}
	count, _ := strconv.Atoi(countStr)
	if count >= DreamingTurnThreshold {
		da.logger.Info("aurora-dream: turn threshold reached", "turns", count)
		return true
	}

	// Check data volume threshold — trigger when stored facts accumulate
	// beyond the threshold, even if time and turns haven't reached limits.
	if factCount, err := da.store.ActiveFactCount(ctx); err == nil && factCount >= DreamingDataThreshold {
		// Only trigger if there has been at least one turn since last dream,
		// to avoid re-triggering immediately after a cycle that didn't reduce
		// fact count below the threshold.
		if count > 0 {
			da.logger.Info("aurora-dream: data volume threshold reached", "facts", factCount, "turns", count)
			return true
		}
	}

	// Check time threshold.
	lastRunStr, err := da.store.GetMeta(ctx, metaLastDreaming)
	if err != nil {
		da.logger.Warn("aurora-dream: failed to read last dreaming time", "error", err)
		return false
	}
	if lastRunStr != "" {
		if lastRun, parseErr := time.Parse(time.RFC3339, lastRunStr); parseErr == nil {
			elapsed := time.Since(lastRun)
			if elapsed.Hours() >= float64(DreamingTimeIntervalH) {
				da.logger.Info("aurora-dream: time threshold reached", "elapsed", elapsed.Round(time.Minute))
				return true
			}
		}
	} else {
		// No previous run recorded — set initial timestamp so the first
		// dreaming cycle fires after the full interval, not immediately.
		_ = da.store.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))
		da.logger.Info("aurora-dream: initialized last-run timestamp")
	}

	return false
}

// RunDream executes a full dreaming cycle and returns the report.
func (da *DreamingAdapter) RunDream(ctx context.Context) (*autonomous.DreamReport, error) {
	report, err := RunDreamingCycle(ctx, da.store, da.embedder, da.client, da.model, da.logger)
	if err != nil {
		return nil, err
	}

	// Reset turn counter and update last run time on success.
	_ = da.store.SetMeta(ctx, metaTurnCount, "0")
	_ = da.store.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))

	return &autonomous.DreamReport{
		FactsVerified:     report.FactsVerified,
		FactsMerged:       report.FactsMerged,
		FactsExpired:      report.FactsExpired,
		PatternsExtracted: report.PatternsExtracted,
		DurationMs:        report.Duration.Milliseconds(),
	}, nil
}
