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

// LifecycleManager orchestrates channel plugin lifecycle (start/stop/health).
type LifecycleManager struct {
	registry  *Registry
	logger    *slog.Logger
	mu        sync.RWMutex
	startedAt map[string]int64 // channel ID → start timestamp
}

// NewLifecycleManager creates a lifecycle manager for the given registry.
func NewLifecycleManager(registry *Registry, logger *slog.Logger) *LifecycleManager {
	return &LifecycleManager{
		registry:  registry,
		logger:    logger,
		startedAt: make(map[string]int64),
	}
}

// StartAll starts all registered channel plugins concurrently.
// Returns nil when all succeed; a non-nil map for any failures.
func (lm *LifecycleManager) StartAll(ctx context.Context) map[string]error {
	plugins := lm.registry.Snapshot()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		errs   map[string]error
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
			lm.logger.Info("starting channel", "id", id)
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

// HealthCheck performs a health check on all channels.
func (lm *LifecycleManager) HealthCheck() []ChannelHealth {
	plugins := lm.registry.Snapshot()

	// Snapshot startedAt once to avoid per-channel lock acquisition.
	lm.mu.RLock()
	startedSnap := make(map[string]int64, len(lm.startedAt))
	for id, ts := range lm.startedAt {
		startedSnap[id] = ts
	}
	lm.mu.RUnlock()

	results := make([]ChannelHealth, 0, len(plugins))
	for id, p := range plugins {
		start := time.Now()
		status := p.Status()
		latency := time.Since(start).Milliseconds()

		results = append(results, ChannelHealth{
			ID:        id,
			Connected: status.Connected,
			Error:     status.Error,
			StartedAt: startedSnap[id],
			Latency:   latency,
		})
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
	lm.mu.Lock()
	lm.startedAt[id] = time.Now().UnixMilli()
	lm.mu.Unlock()
	return nil
}

// GetStartedAt returns the start timestamp (unix ms) for a channel, or 0 if unknown.
func (lm *LifecycleManager) GetStartedAt(id string) int64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.startedAt[id]
}

// RestartChannel stops and restarts a single channel.
func (lm *LifecycleManager) RestartChannel(ctx context.Context, id string) error {
	if err := lm.StopChannel(ctx, id); err != nil {
		lm.logger.Warn("channel stop failed during restart", "id", id, "error", err)
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
	lm.mu.Lock()
	delete(lm.startedAt, id)
	lm.mu.Unlock()
	return nil
}
