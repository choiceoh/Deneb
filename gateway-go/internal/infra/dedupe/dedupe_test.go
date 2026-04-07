package dedupe

import (
	"fmt"
	"testing"
	"time"
)

func TestCheckNewID(t *testing.T) {
	tr := NewTracker(time.Minute, 100)
	defer tr.Close()
	if !tr.Check("req-1") {
		t.Error("first check should return true (new)")
	}
}

func TestCheckDuplicate(t *testing.T) {
	tr := NewTracker(time.Minute, 100)
	defer tr.Close()
	tr.Check("req-1")
	if tr.Check("req-1") {
		t.Error("second check should return false (duplicate)")
	}
}

func TestCheckExpired(t *testing.T) {
	tr := NewTracker(10*time.Millisecond, 100)
	defer tr.Close()
	tr.Check("req-1")
	time.Sleep(20 * time.Millisecond)
	if !tr.Check("req-1") {
		t.Error("expired ID should be treated as new")
	}
}

func TestMaxSizeEviction(t *testing.T) {
	tr := NewTracker(time.Minute, 3)
	defer tr.Close()
	tr.Check("a")
	tr.Check("b")
	tr.Check("c")
	// Adding a 4th should evict the oldest.
	if !tr.Check("d") {
		t.Error("new ID should succeed")
	}
	if tr.Len() > 3 {
		t.Errorf("len = %d, want <= 3", tr.Len())
	}
}

func TestBackgroundGC(t *testing.T) {
	// TTL of 50ms, GC runs at 25ms interval.
	// Use generous sleep to avoid flakiness under race detector.
	tr := NewTracker(50*time.Millisecond, 100)
	defer tr.Close()
	tr.Check("gc-1")
	tr.Check("gc-2")
	if tr.Len() != 2 {
		t.Fatalf("expected 2, got %d", tr.Len())
	}
	// Wait well beyond TTL + GC interval for sweep to complete.
	time.Sleep(300 * time.Millisecond)
	if tr.Len() != 0 {
		t.Errorf("after GC, len = %d, want 0", tr.Len())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tr := NewTracker(time.Minute, 1000)
	defer tr.Close()
	done := make(chan struct{})
	for i := range 100 {
		go func(i int) {
			tr.Check(fmt.Sprintf("req-%d", i))
			done <- struct{}{}
		}(i)
	}
	for range 100 {
		<-done
	}
	if tr.Len() != 100 {
		t.Errorf("len = %d, want 100", tr.Len())
	}
}

func TestClose(t *testing.T) {
	tr := NewTracker(time.Minute, 100)
	tr.Close()
	// Double close should not panic.
	tr.Close()
}
