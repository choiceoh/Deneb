package cron

import (
	"testing"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		input   string
		wantMs  int64
		wantErr bool
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

func TestParseSmartSchedule(t *testing.T) {
	tests := []struct {
		input    string
		wantKind string
		wantErr  bool
	}{
		// Interval formats.
		{"30m", "every", false},
		{"1h", "every", false},
		{"every 5m", "every", false},
		{"5000", "every", false},

		// Cron expressions.
		{"0 8 * * *", "cron", false},
		{"*/5 * * * *", "cron", false},
		{"0 0 1 * *", "cron", false},
		{"0 9 * * mon-fri", "cron", false},

		// Cron shorthand aliases.
		{"@daily", "cron", false},
		{"@hourly", "cron", false},
		{"@weekly", "cron", false},
		{"@monthly", "cron", false},
		{"@yearly", "cron", false},

		// ISO 8601 timestamp → at.
		{"2026-04-06T08:00:00", "at", false},
		{"2030-01-01", "at", false},

		// Errors.
		{"", "", true},
		{"invalid junk here", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sched, err := ParseSmartSchedule(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got kind=%q", sched.Kind)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sched.Kind != tt.wantKind {
				t.Fatalf("expected kind=%q, got %q", tt.wantKind, sched.Kind)
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
