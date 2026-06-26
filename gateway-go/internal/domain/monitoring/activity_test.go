package monitoring

import (
	"testing"
	"time"
)

func TestActivityTracker(t *testing.T) {
	tracker := NewActivityTracker()
	initial := tracker.LastActivityAt()
	if initial <= 0 {
		t.Error("expected initial activity timestamp")
	}

	time.Sleep(2 * time.Millisecond)
	tracker.Touch()
	after := tracker.LastActivityAt()
	if after <= initial {
		t.Error("expected updated timestamp after Touch")
	}
}
