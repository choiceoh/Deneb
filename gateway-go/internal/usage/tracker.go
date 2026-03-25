// Package usage tracks API call counts and token usage per provider.
//
// Counters are in-memory and reset on gateway restart, which is acceptable
// for the single-user deployment model.
package usage

import (
	"sync"
	"time"
)

// TokenUsage holds token counts for a single session or provider window.
type TokenUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

// ProviderStats holds per-provider usage counters.
type ProviderStats struct {
	Calls  int64      `json:"calls"`
	Tokens TokenUsage `json:"tokens"`
}

// StatusReport is returned by usage.status.
type StatusReport struct {
	Uptime    string                    `json:"uptime"`
	StartedAt string                   `json:"startedAt"`
	Providers map[string]*ProviderStats `json:"providers"`
}

// CostReport is returned by usage.cost.
type CostReport struct {
	TotalCalls int64                     `json:"totalCalls"`
	Providers  map[string]*ProviderStats `json:"providers"`
}

// Tracker is a thread-safe in-memory usage tracker.
type Tracker struct {
	mu        sync.RWMutex
	providers map[string]*ProviderStats
	startedAt time.Time
}

// New creates a new usage tracker.
func New() *Tracker {
	return &Tracker{
		providers: make(map[string]*ProviderStats),
		startedAt: time.Now(),
	}
}

// RecordCall increments the call counter for a provider.
func (t *Tracker) RecordCall(provider string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.getOrCreate(provider)
	s.Calls++
}

// RecordTokens adds token usage for a provider.
func (t *Tracker) RecordTokens(provider string, input, output, cacheRead, cacheWrite int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.getOrCreate(provider)
	s.Tokens.Input += input
	s.Tokens.Output += output
	s.Tokens.CacheRead += cacheRead
	s.Tokens.CacheWrite += cacheWrite
}

// Status returns the current usage status report.
func (t *Tracker) Status() *StatusReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	providers := make(map[string]*ProviderStats, len(t.providers))
	for k, v := range t.providers {
		cp := *v
		providers[k] = &cp
	}

	return &StatusReport{
		Uptime:    time.Since(t.startedAt).Truncate(time.Second).String(),
		StartedAt: t.startedAt.Format(time.RFC3339),
		Providers: providers,
	}
}

// Cost returns the current cost/token usage report.
func (t *Tracker) Cost() *CostReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var total int64
	providers := make(map[string]*ProviderStats, len(t.providers))
	for k, v := range t.providers {
		cp := *v
		providers[k] = &cp
		total += v.Calls
	}

	return &CostReport{
		TotalCalls: total,
		Providers:  providers,
	}
}

func (t *Tracker) getOrCreate(provider string) *ProviderStats {
	s, ok := t.providers[provider]
	if !ok {
		s = &ProviderStats{}
		t.providers[provider] = s
	}
	return s
}
