package briefcase

import (
	"errors"
	"testing"
	"time"
)

func TestFrozenAndManualClock(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	start := time.Date(2026, 7, 11, 9, 30, 0, 123, loc)
	frozen := NewFrozenClock(start)
	wantUTC := start.UTC()
	if got := frozen.Now(); !got.Equal(wantUTC) || got.Location() != time.UTC {
		t.Fatalf("frozen Now = %v (%v), want %v UTC", got, got.Location(), wantUTC)
	}
	if got := frozen.Now(); !got.Equal(wantUTC) {
		t.Fatalf("frozen clock changed: %v", got)
	}

	manual := NewManualClock(start)
	if err := manual.Advance(90 * time.Second); err != nil {
		t.Fatal(err)
	}
	want := wantUTC.Add(90 * time.Second)
	if got := manual.Now(); !got.Equal(want) {
		t.Fatalf("manual Now = %v, want %v", got, want)
	}
	if err := manual.AdvanceTo(want.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := manual.Advance(-time.Nanosecond); !errors.Is(err, ErrClockRewind) {
		t.Fatalf("negative Advance error = %v, want ErrClockRewind", err)
	}
	if err := manual.AdvanceTo(start); !errors.Is(err, ErrClockRewind) {
		t.Fatalf("backward AdvanceTo error = %v, want ErrClockRewind", err)
	}
}
