package dedupe

import (
	"fmt"
	"testing"
	"time"
)

func TestCheckNewID(t *testing.T) {
	tr := NewTracker(time.Minute, 100)
	if !tr.Check("req-1") {
		t.Error("first check should return true (new)")
	}
}

func TestCheckDuplicate(t *testing.T) {
	tr := NewTracker(time.Minute, 100)
	tr.Check("req-1")
	if tr.Check("req-1") {
		t.Error("second check should return false (duplicate)")
	}
}

func TestCheckExpired(t *testing.T) {
	tr := NewTracker(10*time.Millisecond, 100)
	tr.Check("req-1")
	time.Sleep(20 * time.Millisecond)
	if !tr.Check("req-1") {
		t.Error("expired ID should be treated as new")
	}
}

func TestMaxSizeEviction(t *testing.T) {
	tr := NewTracker(time.Minute, 3)
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

func TestConcurrentAccess(t *testing.T) {
	tr := NewTracker(time.Minute, 1000)
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func(i int) {
			tr.Check(fmt.Sprintf("req-%d", i))
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if tr.Len() != 100 {
		t.Errorf("len = %d, want 100", tr.Len())
	}
}
