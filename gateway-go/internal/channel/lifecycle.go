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
// Returns a map of channel ID → error for any failures.
func (lm *LifecycleManager) StartAll(ctx context.Context) map[string]error {
	lm.registry.mu.RLock()
	plugins := make(map[string]Plugin, len(lm.registry.plugins))
	for id, p := range lm.registry.plugins {
		plugins[id] = p
	}
	lm.registry.mu.RUnlock()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		errors = make(map[string]error)
	)

	for id, p := range plugins {
		wg.Add(1)
		go func(id string, p Plugin) {
			defer wg.Done()
			lm.logger.Info("starting channel", "id", id)
			if err := p.Start(ctx); err != nil {
				mu.Lock()
				errors[id] = err
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
	return errors
}

// StopAll stops all registered channel plugins concurrently.
// Returns a map of channel ID → error for any failures.
func (lm *LifecycleManager) StopAll(ctx context.Context) map[string]error {
	lm.registry.mu.RLock()
	plugins := make(map[string]Plugin, len(lm.registry.plugins))
	for id, p := range lm.registry.plugins {
		plugins[id] = p
	}
	lm.registry.mu.RUnlock()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		errors = make(map[string]error)
	)

	for id, p := range plugins {
		wg.Add(1)
		go func(id string, p Plugin) {
			defer wg.Done()
			lm.logger.Info("stopping channel", "id", id)
			if err := p.Stop(ctx); err != nil {
				mu.Lock()
				errors[id] = err
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
	return errors
}

// HealthCheck performs a health check on all channels.
func (lm *LifecycleManager) HealthCheck() []ChannelHealth {
	lm.registry.mu.RLock()
	plugins := make(map[string]Plugin, len(lm.registry.plugins))
	for id, p := range lm.registry.plugins {
		plugins[id] = p
	}
	lm.registry.mu.RUnlock()

	results := make([]ChannelHealth, 0, len(plugins))
	for id, p := range plugins {
		start := time.Now()
		status := p.Status()
		latency := time.Since(start).Milliseconds()

		lm.mu.RLock()
		startedAt := lm.startedAt[id]
		lm.mu.RUnlock()

		results = append(results, ChannelHealth{
			ID:        id,
			Connected: status.Connected,
			Error:     status.Error,
			StartedAt: startedAt,
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
