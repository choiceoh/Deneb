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
	countStr, _ := da.store.GetMeta(ctx, metaTurnCount)
	count, _ := strconv.Atoi(countStr)
	count++
	_ = da.store.SetMeta(ctx, metaTurnCount, strconv.Itoa(count))
}

// ShouldDream checks if dreaming conditions are met: turn count >= 50 or time >= 8 hours.
func (da *DreamingAdapter) ShouldDream(ctx context.Context) bool {
	// Check turn threshold.
	countStr, _ := da.store.GetMeta(ctx, metaTurnCount)
	count, _ := strconv.Atoi(countStr)
	if count >= DreamingTurnThreshold {
		return true
	}

	// Check time threshold.
	lastRunStr, _ := da.store.GetMeta(ctx, metaLastDreaming)
	if lastRunStr != "" {
		if lastRun, err := time.Parse(time.RFC3339, lastRunStr); err == nil {
			return time.Since(lastRun).Hours() >= float64(DreamingTimeIntervalH)
		}
	} else {
		// No previous run recorded — set initial timestamp so the first
		// dreaming cycle fires after the full interval, not immediately.
		_ = da.store.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))
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
