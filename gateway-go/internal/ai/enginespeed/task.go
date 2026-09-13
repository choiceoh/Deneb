package enginespeed

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// endpointsEnv is the same operator-set variable the per-run prefix-cache
// sampler reads, so one configured engine address serves both. A comma
// separates engines when the fleet serves more than one model locally.
const endpointsEnv = "DENEB_ENGINE_METRICS_URL"

// PollInterval is how often the engine is scraped.
//
// The cumulative series would be happy with an hourly poll — a day's totals are
// the same either way. The PEAK is what sets this cadence: the engine exports
// occupancy as a gauge and keeps no high-water mark, so a spike between two
// scrapes is a spike nobody saw. Fifteen seconds resolves the overlaps this
// fleet actually produces (turns run tens of seconds) while leaving the scrape
// itself a rounding error against the work it measures.
//
// The engine's own doctrine says the same thing about its memory peak: "a
// scrape every fifteen seconds cannot see a four-second cliff". The number
// reported here is a floor, and it is labeled as one.
const PollInterval = 15 * time.Second

// DefaultStatePath is where the day history lives.
func DefaultStatePath() string {
	return filepath.Join(config.ResolveStateDir(), "engine-speed.json")
}

// Endpoints lists the configured engine /metrics endpoints. Empty (the
// variable unset) disables the task entirely — there is no guessing at an
// address here, because a wrong guess is a request to somebody else's server.
func Endpoints() []string {
	raw := strings.TrimSpace(os.Getenv(endpointsEnv))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Task samples the configured engines on a fixed cadence and folds each scrape
// into its local day.
type Task struct {
	store     *Store
	endpoints []string
	logger    *slog.Logger
}

// NewTask returns nil when no engine is configured, so the caller registers
// nothing rather than scheduling a task that can only ever no-op.
func NewTask(store *Store, endpoints []string, logger *slog.Logger) *Task {
	if store == nil || len(endpoints) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Task{store: store, endpoints: endpoints, logger: logger}
}

func (t *Task) Name() string            { return "engine-speed" }
func (t *Task) Interval() time.Duration { return PollInterval }
func (t *Task) Store() *Store           { return t.store }

// Run scrapes every configured engine once.
//
// A down or booting engine contributes nothing and is not an error: engines on
// this fleet restart for deploys and bring-ups, and a sampler that failed the
// cycle would turn every such window into an alarm. The restart itself is
// already visible — as the interval the store discards.
func (t *Task) Run(ctx context.Context) error {
	now := time.Now()
	for _, endpoint := range t.endpoints {
		counters, ok := observe.FetchEngineCounters(ctx, endpoint)
		if !ok {
			continue
		}
		t.store.Observe(endpoint, now, PollInterval, counters)
	}
	if err := t.store.MaybeFlush(now); err != nil {
		t.logger.Warn("engine-speed: persisting the day history failed", "error", err)
	}
	return nil
}
