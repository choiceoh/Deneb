package coremarkdown

import (
	"encoding/json"
	"sync"
)

// ---------------------------------------------------------------------------
// LRU cache (matches markdown_cgo.go pattern)
// ---------------------------------------------------------------------------

const cacheMaxEntries = 128

type cacheEntry struct {
	value      json.RawMessage
	lastAccess int64
}

type irCache struct {
	mu        sync.Mutex
	entries   map[uint64]*cacheEntry
	accessCtr int64
}

var cache = &irCache{entries: make(map[uint64]*cacheEntry)}

func fnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func (c *irCache) get(key uint64) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCtr++
	e.lastAccess = c.accessCtr
	return e.value, true
}

func (c *irCache) put(key uint64, val json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessCtr++
	if len(c.entries) >= cacheMaxEntries {
		var lruKey uint64
		lruAccess := c.accessCtr + 1
		for k, e := range c.entries {
			if e.lastAccess < lruAccess {
				lruAccess = e.lastAccess
				lruKey = k
			}
		}
		delete(c.entries, lruKey)
	}
	c.entries[key] = &cacheEntry{value: val, lastAccess: c.accessCtr}
}
