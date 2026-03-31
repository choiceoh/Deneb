package channel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ChannelHealth represents the health status of a single channel.
type ChannelHealth struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	Latency   int64  `json:"latencyMs,omitempty"`
}

// Restart backoff constants.
const (
	restartBaseDelay  = 1 * time.Second
	restartMaxDelay   = 30 * time.Second
	restartMaxRetries = 5
)

// LifecycleManager orchestrates channel plugin lifecycle (start/stop/health).
type LifecycleManager struct {
	registry      *Registry
	logger        *slog.Logger
	snapshotStore *SnapshotStore // optional; updated on start/stop events

	mu           sync.RWMutex
	startedAt    map[string]int64 // guarded by mu — channel ID → start timestamp
	restartCount map[string]int   // guarded by mu — channel ID → consecutive restart attempts
}

// SetSnapshotStore attaches a SnapshotStore so lifecycle events
// automatically update channel account snapshots.
func (lm *LifecycleManager) SetSnapshotStore(s *SnapshotStore) {
	lm.snapshotStore = s
}

// NewLifecycleManager creates a lifecycle manager for the given registry.
func NewLifecycleManager(registry *Registry, logger *slog.Logger) *LifecycleManager {
	return &LifecycleManager{
		registry:     registry,
		logger:       logger,
		startedAt:    make(map[string]int64),
		restartCount: make(map[string]int),
	}
}

// StartAll starts all registered channel plugins concurrently.
// Returns nil when all succeed; a non-nil map for any failures.
func (lm *LifecycleManager) StartAll(ctx context.Context) map[string]error {
	plugins := lm.registry.Snapshot()

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs map[string]error
	)

	for id, p := range plugins {
		wg.Add(1)
		go func(id string, p Plugin) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					if errs == nil {
						errs = make(map[string]error)
					}
					errs[id] = fmt.Errorf("panic in channel %q Start: %v", id, r)
					mu.Unlock()
					lm.logger.Error("channel start panicked", "id", id, "panic", r)
				}
			}()
			if err := p.Start(ctx); err != nil {
				mu.Lock()
				if errs == nil {
					errs = make(map[string]error)
				}
				errs[id] = err
				mu.Unlock()
				lm.logger.Error("channel start failed", "id", id, "error", err)
				return
			}
			lm.mu.Lock()
			lm.startedAt[id] = time.Now().UnixMilli()
			lm.mu.Unlock()
			lm.logger.Info("channel started", "id", id)
		}(id, p)
	}

	wg.Wait()
	return errs
}

// StopAll stops all registered channel plugins concurrently.
// Returns nil when all succeed; a non-nil map for any failures.
func (lm *LifecycleManager) StopAll(ctx context.Context) map[string]error {
	plugins := lm.registry.Snapshot()

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs map[string]error
	)

	for id, p := range plugins {
		wg.Add(1)
		go func(id string, p Plugin) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					if errs == nil {
						errs = make(map[string]error)
					}
					errs[id] = fmt.Errorf("panic in channel %q Stop: %v", id, r)
					mu.Unlock()
					lm.logger.Error("channel stop panicked", "id", id, "panic", r)
				}
			}()
			lm.logger.Info("stopping channel", "id", id)
			if err := p.Stop(ctx); err != nil {
				mu.Lock()
				if errs == nil {
					errs = make(map[string]error)
				}
				errs[id] = err
				mu.Unlock()
				lm.logger.Error("channel stop failed", "id", id, "error", err)
				return
			}
			lm.mu.Lock()
			delete(lm.startedAt, id)
			lm.mu.Unlock()
			lm.logger.Info("channel stopped", "id", id)
		}(id, p)
	}

	wg.Wait()
	return errs
}

// HealthCheck performs a concurrent health check on all channels.
// Each channel's Status() call runs in its own goroutine so a slow
// channel does not block health reporting for the rest.
func (lm *LifecycleManager) HealthCheck() []ChannelHealth {
	plugins := lm.registry.Snapshot()

	// Snapshot startedAt once to avoid per-channel lock acquisition.
	lm.mu.RLock()
	startedSnap := make(map[string]int64, len(lm.startedAt))
	for id, ts := range lm.startedAt {
		startedSnap[id] = ts
	}
	lm.mu.RUnlock()

	type indexedHealth struct {
		idx    int
		health ChannelHealth
	}

	count := len(plugins)
	if count == 0 {
		return nil
	}

	ch := make(chan indexedHealth, count)
	i := 0
	for id, p := range plugins {
		idx := i
		i++
		go func(id string, p Plugin, idx int) {
			// Recover from panics so a misbehaving plugin cannot deadlock the
			// health-check channel (ch has a fixed capacity of count).
			defer func() {
				if r := recover(); r != nil {
					lm.logger.Error("channel status panicked", "id", id, "panic", r)
					ch <- indexedHealth{
						idx: idx,
						health: ChannelHealth{
							ID:    id,
							Error: fmt.Sprintf("status panic: %v", r),
						},
					}
				}
			}()
			start := time.Now()
			status := p.Status()
			latency := time.Since(start).Milliseconds()
			ch <- indexedHealth{
				idx: idx,
				health: ChannelHealth{
					ID:        id,
					Connected: status.Connected,
					Error:     status.Error,
					StartedAt: startedSnap[id],
					Latency:   latency,
				},
			}
		}(id, p, idx)
	}

	results := make([]ChannelHealth, count)
	for j := 0; j < count; j++ {
		ih := <-ch
		results[ih.idx] = ih.health
	}
	return results
}

// StartChannel starts a single channel by ID.
func (lm *LifecycleManager) StartChannel(ctx context.Context, id string) error {
	p := lm.registry.Get(id)
	if p == nil {
		return fmt.Errorf("channel %q not registered", id)
	}
	if err := p.Start(ctx); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	lm.mu.Lock()
	lm.startedAt[id] = now
	lm.restartCount[id] = 0 // reset on successful start
	lm.mu.Unlock()

	// Update the snapshot store with post-start state.
	if lm.snapshotStore != nil {
		status := p.Status()
		lm.snapshotStore.Update(id, AccountSnapshot{
			AccountID:   id,
			Enabled:     true,
			Running:     true,
			Connected:   status.Connected,
			LastError:   status.Error,
			LastStartAt: now,
		})
	}
	return nil
}

// GetStartedAt returns the start timestamp (unix ms) for a channel, or 0 if unknown.
func (lm *LifecycleManager) GetStartedAt(id string) int64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.startedAt[id]
}

// RestartChannel stops and restarts a single channel with exponential backoff.
// Limits consecutive restart attempts to restartMaxRetries before giving up.
func (lm *LifecycleManager) RestartChannel(ctx context.Context, id string) error {
	if err := lm.StopChannel(ctx, id); err != nil {
		lm.logger.Warn("channel stop failed during restart", "id", id, "error", err)
	}

	lm.mu.Lock()
	attempt := lm.restartCount[id]
	if attempt >= restartMaxRetries {
		lm.mu.Unlock()
		return fmt.Errorf("channel %q exceeded max restart attempts (%d)", id, restartMaxRetries)
	}
	lm.restartCount[id] = attempt + 1
	lm.mu.Unlock()

	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, capped at 30s.
	delay := restartBaseDelay << uint(attempt)
	if delay > restartMaxDelay {
		delay = restartMaxDelay
	}
	lm.logger.Info("restarting channel with backoff", "id", id, "attempt", attempt+1, "delay", delay)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}

	return lm.StartChannel(ctx, id)
}

// StopChannel stops a single channel by ID.
func (lm *LifecycleManager) StopChannel(ctx context.Context, id string) error {
	p := lm.registry.Get(id)
	if p == nil {
		return fmt.Errorf("channel %q not registered", id)
	}
	if err := p.Stop(ctx); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	lm.mu.Lock()
	delete(lm.startedAt, id)
	lm.mu.Unlock()

	// Update the snapshot store with post-stop state.
	if lm.snapshotStore != nil {
		lm.snapshotStore.Update(id, AccountSnapshot{
			AccountID:  id,
			Enabled:    true,
			Running:    false,
			Connected:  false,
			LastStopAt: now,
		})
	}
	return nil
}
