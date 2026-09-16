package routershare

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// PollInterval is how often the router's meter is read. The meter is a
// loopback GET that answers from memory; a minute resolves a day into 1,440
// samples and loses at most a minute of attribution across a gateway restart.
const PollInterval = time.Minute

// Meter resolves the router's base URL and gate token; the same function the
// engine RPC uses, re-read per poll so a hot-reloaded router config counts.
type Meter func() (baseURL, token string, localModels map[string]bool)

// Task samples the router meter on a fixed cadence and files each reading's
// growth under its local day.
type Task struct {
	store  *Store
	meter  Meter
	logger *slog.Logger
}

// NewTask returns nil when there is nothing to sample, so the caller
// registers nothing.
func NewTask(store *Store, meter Meter, logger *slog.Logger) *Task {
	if store == nil || meter == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Task{store: store, meter: meter, logger: logger}
}

func (t *Task) Name() string            { return "router-share" }
func (t *Task) Interval() time.Duration { return PollInterval }
func (t *Task) Store() *Store           { return t.store }

// Run reads the meter once. An unreachable router contributes nothing and is
// not an error — the router restarts on every deploy.
func (t *Task) Run(ctx context.Context) error {
	baseURL, token, _ := t.meter()
	if baseURL == "" {
		return nil
	}
	now := time.Now()
	if usage, ok := observe.FetchRouterUsage(ctx, baseURL, token); ok {
		t.store.Observe(now, usage)
	}
	if err := t.store.MaybeFlush(now); err != nil {
		t.logger.Warn("router-share: persisting the day history failed", "error", err)
	}
	return nil
}
