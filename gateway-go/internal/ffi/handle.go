package ffi

import (
	"runtime"
	"sync"
)

// Handle wraps an opaque handle (uint32) with automatic cleanup.
// Uses Go's GC finalizer as a safety net: if Drop is not called explicitly,
// the finalizer calls the drop function to prevent resource leaks.
// Callers should still prefer explicit Drop() for deterministic cleanup.
//
// Drop() is safe to call concurrently or multiple times; sync.Once guarantees
// the drop function is invoked exactly once.
type Handle struct {
	id     uint32
	dropFn func(uint32)
	once   sync.Once
}

// NewHandle creates a managed Handle. dropFn is called exactly once,
// either via an explicit Drop() call or via the GC finalizer.
func NewHandle(id uint32, dropFn func(uint32)) *Handle {
	h := &Handle{id: id, dropFn: dropFn}
	runtime.SetFinalizer(h, (*Handle).Drop)
	return h
}

// ID returns the raw handle value for passing to FFI functions.
func (h *Handle) ID() uint32 { return h.id }

// Drop releases resources. Safe to call concurrently or multiple times;
// the drop function is invoked exactly once.
func (h *Handle) Drop() {
	h.once.Do(func() {
		h.dropFn(h.id)
	})
	runtime.SetFinalizer(h, nil) // clear finalizer to allow earlier GC; idempotent
}

// NewCompactionSweepHandle creates a compaction sweep and returns a managed Handle.
func NewCompactionSweepHandle(configJSON string, conversationID, tokenBudget uint64, force, hardTrigger bool, nowMs int64) (*Handle, error) {
	id, err := CompactionSweepNew(configJSON, conversationID, tokenBudget, force, hardTrigger, nowMs)
	if err != nil {
		return nil, err
	}
	return NewHandle(id, CompactionSweepDrop), nil
}

// NewContextAssemblyHandle creates a context assembly engine and returns a managed Handle.
func NewContextAssemblyHandle(conversationID, tokenBudget uint64, freshTailCount uint32) (*Handle, error) {
	id, err := ContextAssemblyNew(conversationID, tokenBudget, freshTailCount)
	if err != nil {
		return nil, err
	}
	return NewHandle(id, ContextEngineDrop), nil
}

// NewContextExpandHandle creates a context expand engine and returns a managed Handle.
func NewContextExpandHandle(summaryID string, maxDepth uint32, includeMessages bool, tokenCap uint64) (*Handle, error) {
	id, err := ContextExpandNew(summaryID, maxDepth, includeMessages, tokenCap)
	if err != nil {
		return nil, err
	}
	return NewHandle(id, ContextEngineDrop), nil
}
