// Package metrics provides lightweight RPC call counting for the Deneb gateway.
//
// Only the Counter type is retained — it tracks RPC call counts (method × status)
// and exposes a Snapshot for internal reporting (e.g. monitoring.rpc_zero_calls).
// Prometheus/histogram support was removed; the gateway does not use a scrape pipeline.
//
// No external dependencies — stdlib only.
package metrics

import (
	"strings"
	"sync"
	"sync/atomic"
)

// Counter is a monotonically increasing counter keyed by label values.
type Counter struct {
	mu     sync.RWMutex
	values map[string]*atomic.Int64
}

// NewCounter creates a new labeled counter.
func NewCounter() *Counter {
	return &Counter{
		values: make(map[string]*atomic.Int64),
	}
}

// Inc increments the counter for the given label values.
func (c *Counter) Inc(labelValues ...string) {
	key := strings.Join(labelValues, "\x00")
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		v.Add(1)
		return
	}
	c.mu.Lock()
	if v, ok = c.values[key]; ok {
		c.mu.Unlock()
		v.Add(1)
		return
	}
	v = &atomic.Int64{}
	v.Store(1)
	c.values[key] = v
	c.mu.Unlock()
}

// Snapshot returns a copy of all label-key → value pairs.
// The key is the \x00-joined label values string.
func (c *Counter) Snapshot() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int64, len(c.values))
	for k, v := range c.values {
		out[k] = v.Load()
	}
	return out
}

// RPCRequestsTotal counts RPC calls keyed by "method\x00status\x00code".
// Used by monitoring.rpc_zero_calls to find never-called methods.
var RPCRequestsTotal = NewCounter()
