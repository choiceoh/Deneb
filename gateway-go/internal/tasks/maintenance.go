package tasks

import (
	"context"
	"log/slog"
	"time"
)

// Maintenance constants.
const (
	// ReconcileGraceMs is the grace period before marking an active task as lost.
	ReconcileGraceMs int64 = 5 * 60 * 1000 // 5 minutes

	// RetentionMs is how long terminal tasks are kept before cleanup.
	RetentionMs int64 = 7 * 24 * 60 * 60 * 1000 // 7 days

	// SweepInterval is how often the maintenance loop runs.
	SweepInterval = 60 * time.Second

	// InitialDelay before the first sweep.
	InitialDelay = 5 * time.Second
)

// SessionChecker is a function that returns true if the given session key
// has an active backing session. Used for orphan detection.
type SessionChecker func(sessionKey string) bool

// MaintenanceResult reports what a sweep cycle did.
type MaintenanceResult struct {
	MarkedLost int
	Pruned     int
	Stamped    int
}

// RunMaintenance performs a single maintenance sweep:
// 1. Mark active tasks as lost if their backing session is gone (orphan recovery).
// 2. Stamp cleanup timestamps on terminal tasks that lack them.
// 3. Prune old terminal tasks past the retention window.
func RunMaintenance(reg *Registry, hasSession SessionChecker, now int64) *MaintenanceResult {
	if now == 0 {
		now = NowMs()
	}

	result := &MaintenanceResult{}
	tasks := reg.ListAll()

	for _, t := range tasks {
		// 1. Orphan recovery: mark active tasks as lost if backing session is gone.
		if t.Status.IsActive() && shouldMarkLost(t, hasSession, now) {
			if err := MarkLost(reg, t.TaskID); err == nil {
				result.MarkedLost++
				reg.logger.Warn("task marked lost (orphan)",
					"taskId", t.TaskID, "runtime", t.Runtime,
					"childSession", t.ChildSessionKey)
			}
			continue
		}

		// 2. Stamp cleanup_after on terminal tasks that don't have it.
		if t.Status.IsTerminal() && t.CleanupAfter == 0 {
			t.CleanupAfter = now + RetentionMs
			if err := reg.Put(t); err == nil {
				result.Stamped++
			}
			continue
		}
	}

	// 3. Prune old terminal tasks.
	pruned, err := reg.store.DeleteTerminalBefore(now)
	if err != nil {
		reg.logger.Error("task maintenance: prune failed", "error", err)
	} else if pruned > 0 {
		result.Pruned = int(pruned)
		// Re-sync in-memory state after pruning from store.
		prunedFromMemory := 0
		reg.mu.Lock()
		for id, t := range reg.tasks {
			if t.Status.IsTerminal() && t.CleanupAfter > 0 && t.CleanupAfter < now {
				reg.deindexTask(t)
				delete(reg.tasks, id)
				prunedFromMemory++
			}
		}
		reg.mu.Unlock()
		reg.logger.Info("task maintenance: pruned terminal tasks",
			"fromStore", pruned, "fromMemory", prunedFromMemory)
	}

	return result
}

// shouldMarkLost checks if an active task should be marked as lost.
func shouldMarkLost(t *TaskRecord, hasSession SessionChecker, now int64) bool {
	if hasSession == nil {
		return false
	}

	// Only check tasks that have a backing session.
	key := t.ChildSessionKey
	if key == "" {
		return false
	}

	// If the session still exists, the task is not lost.
	if hasSession(key) {
		return false
	}

	// Grace period: don't mark as lost if the task was recently active.
	ref := taskReferenceAt(t)
	return (now - ref) >= ReconcileGraceMs
}

// StartMaintenanceLoop runs periodic maintenance sweeps in the background.
func StartMaintenanceLoop(ctx context.Context, reg *Registry, hasSession SessionChecker, logger *slog.Logger) {
	go func() {
		// Wait before first sweep.
		select {
		case <-ctx.Done():
			return
		case <-time.After(InitialDelay):
		}

		ticker := time.NewTicker(SweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := NowMs()
				result := RunMaintenance(reg, hasSession, now)
				if result.MarkedLost > 0 || result.Pruned > 0 || result.Stamped > 0 {
					logger.Info("task maintenance sweep",
						"lost", result.MarkedLost,
						"pruned", result.Pruned,
						"stamped", result.Stamped)
				}
			}
		}
	}()
}
