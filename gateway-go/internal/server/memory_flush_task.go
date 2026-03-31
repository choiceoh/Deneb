// memory_flush_task.go — periodic task that flushes high-importance memory
// facts to date-stamped markdown files. Registered with the autonomous service.
package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

// memoryFlushTask implements autonomous.PeriodicTask to periodically call
// Store.FlushToDateFile, writing new high-importance facts to
// ~/.deneb/memory/YYYY-MM-DD.md.
type memoryFlushTask struct {
	store    *memory.Store
	dir      string // base directory (e.g., ~/.deneb)
	timezone string // IANA timezone for date-stamped filenames
	logger   *slog.Logger
}

func (t *memoryFlushTask) Name() string            { return "memory-flush" }
func (t *memoryFlushTask) Interval() time.Duration { return 30 * time.Minute }

func (t *memoryFlushTask) Run(ctx context.Context) error {
	flushed, err := t.store.FlushToDateFile(ctx, t.dir, t.timezone)
	if err != nil {
		return err
	}
	if flushed > 0 {
		t.logger.Info("memory flush completed", "flushed", flushed)
	}
	return nil
}
