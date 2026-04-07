// Package corecache provides generic cache primitives.
package corecache

import (
	"container/list"
	"sync"
	"time"
)

// LRU is a generic bounded cache with O(1) get/put/evict and optional TTL.
// Thread-safe via sync.Mutex.
type LRU[K comparable, V any] struct {
	mu       sync.Mutex
	items    map[K]*list.Element
	order    *list.List // front = oldest access, back = newest
	capacity int
	ttl      time.Duration // 0 = no expiry
}

type lruEntry[K comparable, V any] struct {
	key       K
	value     V
	createdAt time.Time
}

// NewLRU creates a bounded LRU cache. ttl of 0 disables time-based expiry.
func NewLRU[K comparable, V any](capacity int, ttl time.Duration) *LRU[K, V] {
	if capacity <= 0 {
		capacity = 64
	}
	return &LRU[K, V]{
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
		capacity: capacity,
		ttl:      ttl,
	}
}

// Get returns the value for key if present and not expired.
// Promotes the entry to most-recently-used on hit.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}

	e := elem.Value.(*lruEntry[K, V])
	if c.ttl > 0 && time.Since(e.createdAt) > c.ttl {
		c.order.Remove(elem)
		delete(c.items, key)
		var zero V
		return zero, false
	}

	c.order.MoveToBack(elem)
	return e.value, true
}

// Put stores a value. Evicts the least-recently-used entry if at capacity.
func (c *LRU[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if elem, exists := c.items[key]; exists {
		e := elem.Value.(*lruEntry[K, V])
		e.value = value
		e.createdAt = now
		c.order.MoveToBack(elem)
		return
	}

	for len(c.items) >= c.capacity {
		front := c.order.Front()
		if front == nil {
			break
		}
		oldest := front.Value.(*lruEntry[K, V])
		c.order.Remove(front)
		delete(c.items, oldest.key)
	}

	e := &lruEntry[K, V]{key: key, value: value, createdAt: now}
	elem := c.order.PushBack(e)
	c.items[key] = elem
}

// Delete removes an entry by key.
func (c *LRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.Remove(elem)
		delete(c.items, key)
	}
}

// Len returns the number of entries (including expired but not yet cleaned).
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Clear removes all entries.
func (c *LRU[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]*list.Element, c.capacity)
	c.order.Init()
}

// PruneFunc removes entries for which fn returns true. Returns count removed.
// Useful for custom eviction policies (e.g., per-entry TTL checking).
func (c *LRU[K, V]) PruneFunc(fn func(key K, value V) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for elem := c.order.Front(); elem != nil; {
		next := elem.Next()
		e := elem.Value.(*lruEntry[K, V])
		if fn(e.key, e.value) {
			c.order.Remove(elem)
			delete(c.items, e.key)
			removed++
		}
		elem = next
	}
	return removed
}

// Cleanup removes expired entries and returns the count removed.
// No-op if TTL is 0. Intended for periodic background cleanup.
func (c *LRU[K, V]) Cleanup() int {
	if c.ttl <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	cutoff := time.Now().Add(-c.ttl)
	for elem := c.order.Front(); elem != nil; {
		next := elem.Next()
		e := elem.Value.(*lruEntry[K, V])
		if e.createdAt.Before(cutoff) {
			c.order.Remove(elem)
			delete(c.items, e.key)
			removed++
		}
		elem = next
	}
	return removed
}
