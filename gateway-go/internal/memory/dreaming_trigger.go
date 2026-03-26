// dreaming_trigger.go — Triggers dreaming cycles based on turn count or time interval.
// Tracks turn count in SQLite metadata and fires dreaming asynchronously.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const metaTurnCount = "dreaming_turn_count"
const metaLastDreaming = "dreaming_last_run"

// DreamingTrigger manages the conditions for triggering dreaming cycles.
type DreamingTrigger struct {
	store    *Store
	embedder *Embedder
	client   *llm.Client
	model    string
	logger   *slog.Logger

	mu       sync.Mutex
	running  bool
}

// NewDreamingTrigger creates a new dreaming trigger.
func NewDreamingTrigger(store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) *DreamingTrigger {
	return &DreamingTrigger{
		store:    store,
		embedder: embedder,
		client:   client,
		model:    model,
		logger:   logger,
	}
}

// IncrementTurnAndCheck increments the turn counter and checks if dreaming should fire.
// If conditions are met, launches dreaming asynchronously and returns true.
func (dt *DreamingTrigger) IncrementTurnAndCheck(ctx context.Context) bool {
	dt.mu.Lock()
	if dt.running {
		dt.mu.Unlock()
		return false
	}
	dt.mu.Unlock()

	// Increment turn count.
	countStr, _ := dt.store.GetMeta(ctx, metaTurnCount)
	count, _ := strconv.Atoi(countStr)
	count++
	_ = dt.store.SetMeta(ctx, metaTurnCount, strconv.Itoa(count))

	// Check turn threshold.
	if count >= DreamingTurnThreshold {
		dt.triggerAsync()
		return true
	}

	// Check time threshold.
	lastRunStr, _ := dt.store.GetMeta(ctx, metaLastDreaming)
	if lastRunStr != "" {
		if lastRun, err := time.Parse(time.RFC3339, lastRunStr); err == nil {
			if time.Since(lastRun).Hours() >= float64(DreamingTimeIntervalH) {
				dt.triggerAsync()
				return true
			}
		}
	} else {
		// No previous run recorded — set initial timestamp.
		_ = dt.store.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))
	}

	return false
}

// triggerAsync launches a dreaming cycle in a background goroutine.
func (dt *DreamingTrigger) triggerAsync() {
	dt.mu.Lock()
	if dt.running {
		dt.mu.Unlock()
		return
	}
	dt.running = true
	dt.mu.Unlock()

	go func() {
		defer func() {
			dt.mu.Lock()
			dt.running = false
			dt.mu.Unlock()
		}()

		ctx := context.Background()
		report, err := RunDreamingCycle(ctx, dt.store, dt.embedder, dt.client, dt.model, dt.logger)
		if err != nil {
			dt.logger.Error("dreaming: cycle failed", "error", err)
			return
		}

		// Reset turn counter and update last run time.
		_ = dt.store.SetMeta(ctx, metaTurnCount, "0")
		_ = dt.store.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))

		dt.logger.Info("dreaming: async cycle finished",
			"verified", report.FactsVerified,
			"merged", report.FactsMerged,
			"expired", report.FactsExpired,
			"patterns", report.PatternsExtracted,
			"duration", fmt.Sprintf("%.1fs", report.Duration.Seconds()),
		)
	}()
}

// StartPeriodicTimer starts a background timer that checks dreaming conditions
// every DreamingTimeIntervalH hours. Call this at gateway startup.
func (dt *DreamingTrigger) StartPeriodicTimer(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Duration(DreamingTimeIntervalH) * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dt.logger.Info("dreaming: periodic timer fired")
				dt.triggerAsync()
			}
		}
	}()
}
