package ffi

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// TestHandleDropOnce verifies that the Rust drop function is called exactly
// once even when Drop() is invoked concurrently from multiple goroutines.
func TestHandleDropOnce(t *testing.T) {
	var callCount atomic.Int32
	mockDrop := func(_ uint32) { callCount.Add(1) }

	h := NewHandle(42, mockDrop)

	const concurrency = 64
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			h.Drop()
		}()
	}
	wg.Wait()

	if n := callCount.Load(); n != 1 {
		t.Errorf("drop function called %d times; want exactly 1", n)
	}
}

// TestHandleDropThenFinalizer verifies that if Drop() is called explicitly,
// the GC finalizer does not trigger a second drop.
func TestHandleDropThenFinalizer(t *testing.T) {
	var callCount atomic.Int32
	mockDrop := func(_ uint32) { callCount.Add(1) }

	func() {
		h := NewHandle(1, mockDrop)
		h.Drop() // explicit drop; finalizer should be cleared
	}()

	// Force GC multiple times to give the finalizer a chance to run if present.
	for range 5 {
		runtime.GC()
	}

	if n := callCount.Load(); n != 1 {
		t.Errorf("drop function called %d times after explicit Drop + GC; want exactly 1", n)
	}
}

// TestHandleFinalizerOnly verifies that if Drop() is never called explicitly,
// the GC finalizer calls the drop function exactly once.
func TestHandleFinalizerOnly(t *testing.T) {
	var callCount atomic.Int32
	mockDrop := func(_ uint32) { callCount.Add(1) }

	func() {
		_ = NewHandle(2, mockDrop)
		// No explicit Drop(); rely solely on the finalizer.
	}()

	for range 5 {
		runtime.GC()
	}

	if n := callCount.Load(); n != 1 {
		t.Errorf("drop function called %d times via finalizer; want exactly 1", n)
	}
}

// TestHandleID verifies ID() returns the raw handle value unchanged.
func TestHandleID(t *testing.T) {
	h := NewHandle(99, func(_ uint32) {})
	defer h.Drop()
	if h.ID() != 99 {
		t.Errorf("ID() = %d; want 99", h.ID())
	}
}
