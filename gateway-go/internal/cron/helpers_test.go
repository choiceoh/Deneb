package cron

import (
	"testing"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		input    string
		wantMs   int64
		wantErr  bool
	}{
		{"5000", 5000, false},
		{"every 5m", 300000, false},
		{"every 1h", 3600000, false},
		{"every 30s", 30000, false},
		{"30s", 30000, false},
		{"1m", 60000, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"every -1s", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sched, err := ParseSchedule(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sched.IntervalMs != tt.wantMs {
				t.Fatalf("expected %d ms, got %d", tt.wantMs, sched.IntervalMs)
			}
		})
	}
}

func TestSchedulerRunning(t *testing.T) {
	s := NewScheduler(nil)
	if s.Running() {
		t.Fatal("expected not running with no tasks")
	}
}
