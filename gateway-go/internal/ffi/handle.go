package ffi

import "runtime"

// Handle wraps a Rust-side opaque handle (uint32) with automatic cleanup.
// If Drop is not called explicitly, the finalizer releases the Rust resources
// when the Handle is garbage-collected, preventing resource leaks.
type Handle struct {
	id      uint32
	dropFn  func(uint32)
	dropped bool
}

// NewHandle creates a managed Handle. The dropFn is called exactly once,
// either via an explicit Drop() call or via the GC finalizer.
func NewHandle(id uint32, dropFn func(uint32)) *Handle {
	h := &Handle{id: id, dropFn: dropFn}
	runtime.SetFinalizer(h, func(h *Handle) { h.Drop() })
	return h
}

// ID returns the raw handle value for passing to FFI functions.
func (h *Handle) ID() uint32 { return h.id }

// Drop releases the Rust-side resources. Safe to call multiple times.
func (h *Handle) Drop() {
	if h.dropped {
		return
	}
	h.dropped = true
	h.dropFn(h.id)
	runtime.SetFinalizer(h, nil)
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
