package monitoring

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestActivityTrackerTouchSessionAndBlankPreservation(t *testing.T) {
	tracker := NewActivityTracker()
	initial := tracker.LastActivityAt()
	if tracker.LastSessionKey() != "" {
		t.Fatalf("initial session = %q", tracker.LastSessionKey())
	}
	time.Sleep(2 * time.Millisecond)
	tracker.TouchSession("client:main")
	if tracker.LastSessionKey() != "client:main" || tracker.LastActivityAt() <= initial {
		t.Fatalf("TouchSession state key=%q timestamp=%d initial=%d", tracker.LastSessionKey(), tracker.LastActivityAt(), initial)
	}
	withSession := tracker.LastActivityAt()
	time.Sleep(2 * time.Millisecond)
	tracker.TouchSession("")
	if tracker.LastSessionKey() != "client:main" {
		t.Fatalf("blank session erased key: %q", tracker.LastSessionKey())
	}
	if tracker.LastActivityAt() <= withSession {
		t.Fatal("blank TouchSession did not update activity timestamp")
	}
}

func TestActivityTrackerZeroValueIsUsable(t *testing.T) {
	var tracker ActivityTracker
	if tracker.LastActivityAt() != 0 || tracker.LastSessionKey() != "" {
		t.Fatalf("zero tracker timestamp=%d key=%q", tracker.LastActivityAt(), tracker.LastSessionKey())
	}
	tracker.Touch()
	if tracker.LastActivityAt() <= 0 {
		t.Fatal("zero tracker Touch did not initialize timestamp")
	}
	tracker.TouchSession("session")
	if tracker.LastSessionKey() != "session" {
		t.Fatalf("zero tracker session = %q", tracker.LastSessionKey())
	}
}

func TestActivityTrackerConcurrentTouches(t *testing.T) {
	tracker := NewActivityTracker()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if i%3 == 0 {
					tracker.Touch()
				} else if i%3 == 1 {
					tracker.TouchSession(fmt.Sprintf("session-%d", (i+j)%10))
				} else {
					_ = tracker.LastActivityAt()
					_ = tracker.LastSessionKey()
				}
			}
		}(i)
	}
	wg.Wait()
	if tracker.LastActivityAt() <= 0 || tracker.LastSessionKey() == "" {
		t.Fatalf("final tracker timestamp=%d key=%q", tracker.LastActivityAt(), tracker.LastSessionKey())
	}
}
