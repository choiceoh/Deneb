package cron

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// resetDentimeForTest ensures each test starts with a clean dentime state.
// Sets the config to empty + resets cache. Registered via t.Cleanup so the
// process-global dentime doesn't leak across tests.
func resetDentimeForTest(t *testing.T) {
	t.Helper()
	t.Setenv("DENEB_TIMEZONE", "")
	dentime.SetConfigTimezone("")
	dentime.ResetCache()
	t.Cleanup(func() {
		dentime.SetConfigTimezone("")
		dentime.ResetCache()
	})
}

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

func TestParseIntervalMs(t *testing.T) {
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
			ms, err := parseIntervalMs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			testutil.NoError(t, err)
			if ms != tt.wantMs {
				t.Fatalf("got %d, want %d ms", ms, tt.wantMs)
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
					t.Fatalf("got kind=%q, want error", sched.Kind)
				}
				return
			}
			testutil.NoError(t, err)
			if sched.Kind != tt.wantKind {
				t.Fatalf("got %q, want kind=%q", sched.Kind, tt.wantKind)
			}
		})
	}
}

// --- resolveScheduleLocation / per-job TZ fallback ---------------------

// TestResolveScheduleLocation_ExplicitJobTzWins verifies per-job Tz beats
// both the dentime global and time.Local.
func TestResolveScheduleLocation_ExplicitJobTzWins(t *testing.T) {
	resetDentimeForTest(t)
	dentime.SetConfigTimezone("Asia/Seoul")
	dentime.ResetCache()

	loc := resolveScheduleLocation("America/Los_Angeles")
	if loc == nil || loc.String() != "America/Los_Angeles" {
		t.Fatalf("per-job Tz lost: got %v, want America/Los_Angeles", loc)
	}
}

// TestResolveScheduleLocation_InheritsDentime verifies empty per-job Tz
// inherits the dentime global zone — the core bug fix.
func TestResolveScheduleLocation_InheritsDentime(t *testing.T) {
	resetDentimeForTest(t)
	dentime.SetConfigTimezone("Asia/Seoul")
	dentime.ResetCache()

	loc := resolveScheduleLocation("")
	if loc == nil || loc.String() != "Asia/Seoul" {
		t.Fatalf("dentime fallback lost: got %v, want Asia/Seoul", loc)
	}
}

// TestResolveScheduleLocation_InvalidTzUsesUTC verifies that a typo'd per-job
// Tz resolves to UTC, not to the inherited global zone. Prevents schedule
// drift from silent bad strings.
func TestResolveScheduleLocation_InvalidTzUsesUTC(t *testing.T) {
	resetDentimeForTest(t)
	dentime.SetConfigTimezone("Asia/Seoul")
	dentime.ResetCache()

	loc := resolveScheduleLocation("Mars/Olympus_Mons")
	if loc == nil || loc.String() != "UTC" {
		t.Fatalf("invalid Tz should map to UTC, got %v", loc)
	}
}

// TestResolveScheduleLocation_NoGlobalFallsBackToLocal verifies the final
// fallback when neither per-job nor dentime provides a zone.
func TestResolveScheduleLocation_NoGlobalFallsBackToLocal(t *testing.T) {
	resetDentimeForTest(t)
	// Intentionally leave dentime empty.

	loc := resolveScheduleLocation("")
	if loc == nil {
		t.Fatal("expected time.Local fallback, got nil")
	}
	// time.Local may be UTC on CI or arbitrary locally; just assert non-nil.
}

// TestComputeNextCronMs_InheritsDentimeZone is the end-to-end behaviour
// check: a "9am daily" cron with empty per-job Tz fires at 09:00 KST when
// dentime is set to Asia/Seoul.
func TestComputeNextCronMs_InheritsDentimeZone(t *testing.T) {
	resetDentimeForTest(t)
	dentime.SetConfigTimezone("Asia/Seoul")
	dentime.ResetCache()

	// Start at 2026-01-15 08:00 KST (=23:00 UTC prev day). A 9am daily
	// should fire 1h later at 09:00 KST = 00:00 UTC.
	kst, _ := time.LoadLocation("Asia/Seoul")
	start := time.Date(2026, 1, 15, 8, 0, 0, 0, kst)

	sched := StoreSchedule{Kind: "cron", Expr: "0 9 * * *"} // empty Tz
	nextMs := computeNextCronMs(sched, start.UnixMilli())
	if nextMs == 0 {
		t.Fatal("computeNextCronMs returned 0 (parse or tz failure)")
	}

	next := time.UnixMilli(nextMs).In(kst)
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Fatalf("next fire = %s, want 09:00 KST", next.Format(time.RFC3339))
	}
	if !next.After(start) {
		t.Fatalf("next should be after start: next=%s start=%s", next, start)
	}
}
