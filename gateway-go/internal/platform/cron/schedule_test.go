package cron

import (
	"testing"
	"time"
)

func TestComputeNextRunAtMs_Every(t *testing.T) {
	now := int64(1000000)
	schedule := StoreSchedule{Kind: "every", EveryMs: 60000}

	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
	if next != now+60000 {
		t.Errorf("next = %d, want %d", next, now+60000)
	}
}

func TestComputeNextRunAtMs_EveryWithAnchor(t *testing.T) {
	anchor := int64(500000)
	now := int64(619999) // just before the 2nd step boundary
	schedule := StoreSchedule{Kind: "every", EveryMs: 60000, AnchorMs: anchor}

	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
	if next != 620000 {
		t.Errorf("next = %d, want 620000", next)
	}
}

func TestComputeNextRunAtMs_At(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).UnixMilli()
	schedule := StoreSchedule{Kind: "at", At: time.UnixMilli(future).Format(time.RFC3339)}

	next := ComputeNextRunAtMs(schedule, time.Now().UnixMilli())
	if next <= 0 {
		t.Error("expected future timestamp")
	}
}

func TestComputeNextRunAtMs_AtPast(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).UnixMilli()
	schedule := StoreSchedule{Kind: "at", At: time.UnixMilli(past).Format(time.RFC3339)}

	next := ComputeNextRunAtMs(schedule, time.Now().UnixMilli())
	if next != 0 {
		t.Errorf("got %d, want 0 for past 'at' schedule", next)
	}
}

func TestComputeNextRunAtMs_Cron(t *testing.T) {
	// Every minute cron.
	schedule := StoreSchedule{Kind: "cron", Expr: "* * * * *"}
	now := time.Now().UnixMilli()

	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
	// Should be within 60 seconds.
	if next-now > 61000 {
		t.Errorf("next = %d, too far from now %d", next, now)
	}
}

func TestComputeNextRunAtMs_CronHourly(t *testing.T) {
	// Top of every hour.
	schedule := StoreSchedule{Kind: "cron", Expr: "0 * * * *"}
	now := time.Now().UnixMilli()

	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
}

func TestParseCronField(t *testing.T) {
	tests := []struct {
		field string
		min   int
		max   int
		want  int // expected number of values
	}{
		{"*", 0, 59, 60},
		{"5", 0, 59, 1},
		{"1,3,5", 0, 59, 3},
		{"1-5", 0, 59, 5},
		{"*/15", 0, 59, 4},
		{"10-30/5", 0, 59, 5},
	}
	for _, tt := range tests {
		result := parseCronField(tt.field, tt.min, tt.max)
		if result == nil {
			t.Errorf("parseCronField(%q) returned nil", tt.field)
			continue
		}
		count := 0
		for _, v := range result {
			if v {
				count++
			}
		}
		if count != tt.want {
			t.Errorf("parseCronField(%q) = %d values, want %d", tt.field, count, tt.want)
		}
	}
}

func TestIsRecurringTopOfHourCronExpr(t *testing.T) {
	tests := []struct {
		expr string
		want bool
	}{
		{"0 * * * *", true},
		{"0 */2 * * *", true},
		{"5 * * * *", false},
		{"*/15 * * * *", false},
		{"0 0 * * * *", true}, // 6-field: sec=0 min=0 hour=*
	}
	for _, tt := range tests {
		got := IsRecurringTopOfHourCronExpr(tt.expr)
		if got != tt.want {
			t.Errorf("IsRecurringTopOfHourCronExpr(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestParseAbsoluteTimeMs(t *testing.T) {
	tests := []struct {
		input string
		want  bool // just check > 0
	}{
		{"1700000000000", true},
		{"2024-01-15T12:00:00Z", true},
		{"2024-01-15", true},
		{"", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		got := parseAbsoluteTimeMs(tt.input)
		if (got > 0) != tt.want {
			t.Errorf("parseAbsoluteTimeMs(%q) = %d, want >0=%v", tt.input, got, tt.want)
		}
	}
}

func TestCronShorthandAliases(t *testing.T) {
	now := time.Now().UnixMilli()
	shorthands := []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually"}
	for _, sh := range shorthands {
		schedule := StoreSchedule{Kind: "cron", Expr: sh}
		next := ComputeNextRunAtMs(schedule, now)
		if next <= now {
			t.Errorf("%s: next = %d, should be > %d", sh, next, now)
		}
	}
}

func TestCronNamedMonths(t *testing.T) {
	now := time.Now().UnixMilli()
	schedule := StoreSchedule{Kind: "cron", Expr: "0 0 1 JAN *"}
	next := ComputeNextRunAtMs(schedule, now)
	if next <= 0 {
		t.Error("expected valid next-run for JAN expression")
	}
}

func TestCronNamedDays(t *testing.T) {
	now := time.Now().UnixMilli()
	schedule := StoreSchedule{Kind: "cron", Expr: "0 0 * * MON"}
	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
	// Verify it's actually a Monday.
	nextTime := time.UnixMilli(next)
	if nextTime.Weekday() != time.Monday {
		t.Errorf("got %s, want Monday", nextTime.Weekday())
	}
}

func TestCronNamedRange(t *testing.T) {
	now := time.Now().UnixMilli()
	schedule := StoreSchedule{Kind: "cron", Expr: "0 9 * * MON-FRI"}
	next := ComputeNextRunAtMs(schedule, now)
	if next <= now {
		t.Errorf("next = %d, should be > %d", next, now)
	}
	// Verify it's a weekday.
	nextTime := time.UnixMilli(next)
	dow := nextTime.Weekday()
	if dow == time.Saturday || dow == time.Sunday {
		t.Errorf("got %s, want weekday", dow)
	}
}

func TestStableJobOffset(t *testing.T) {
	offset1 := stableJobOffset("0 * * * *", 300000)
	offset2 := stableJobOffset("0 * * * *", 300000)
	if offset1 != offset2 {
		t.Error("same expr should give same offset")
	}
	if offset1 < 0 || offset1 >= 300000 {
		t.Errorf("offset %d out of range [0, 300000)", offset1)
	}

	// Different expr should (likely) give different offset.
	offset3 := stableJobOffset("30 * * * *", 300000)
	// Not guaranteed but very unlikely to collide.
	_ = offset3
}
