package lanewatch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func fixedClock(t0 *time.Time) func() time.Time { return func() time.Time { return *t0 } }

func lane(name string, budget time.Duration, r func() (Reading, error)) Lane {
	return Lane{Name: name, MaxSilence: budget, Read: func(context.Context) (Reading, error) { return r() }}
}

// A watch that fires on healthy idleness gets muted, and then it is worth
// nothing. A lane that vouches for its own zero must never be reported.
func TestIdleLaneIsNeverReported(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	w := New(slog.New(slog.DiscardHandler), fixedClock(&now),
		lane("mail", time.Hour, func() (Reading, error) { return Reading{Worked: 0, Idle: true}, nil }))

	for i := 0; i < 5; i++ {
		now = now.Add(24 * time.Hour)
		if f := w.Check(context.Background()); len(f) != 0 {
			t.Fatalf("idle lane reported after %d days: %v", i+1, f)
		}
	}
}

// Silence past the lane's own budget is the finding this package exists for.
func TestSilentLaneReportsAfterItsBudget(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	w := New(slog.New(slog.DiscardHandler), fixedClock(&now),
		lane("evolve", 48*time.Hour, func() (Reading, error) {
			return Reading{Worked: 0, Detail: "evolved=0"}, nil
		}))

	now = now.Add(24 * time.Hour)
	if f := w.Check(context.Background()); len(f) != 0 {
		t.Fatalf("reported inside the budget: %v", f)
	}
	now = now.Add(36 * time.Hour)
	f := w.Check(context.Background())
	if len(f) != 1 || f[0].Lane != "evolve" {
		t.Fatalf("findings = %v, want one for evolve", f)
	}
	if f[0].Detail != "evolved=0" {
		t.Errorf("detail = %q, want the lane's own", f[0].Detail)
	}
}

// Work resets the clock — a lane that recovers stops being reported.
func TestWorkResetsTheSilenceClock(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	worked := 0
	w := New(slog.New(slog.DiscardHandler), fixedClock(&now),
		lane("dream", 12*time.Hour, func() (Reading, error) { return Reading{Worked: worked}, nil }))

	now = now.Add(24 * time.Hour)
	if len(w.Check(context.Background())) != 1 {
		t.Fatal("expected a finding while silent")
	}
	worked = 1
	now = now.Add(time.Hour)
	if f := w.Check(context.Background()); len(f) != 0 {
		t.Fatalf("recovered lane still reported: %v", f)
	}
	worked = 0
	now = now.Add(6 * time.Hour)
	if f := w.Check(context.Background()); len(f) != 0 {
		t.Fatalf("clock did not reset — reported %v after only 6h of new silence", f)
	}
}

// A lane whose liveness cannot be READ is not a lane known to be healthy.
func TestUnreadableLaneIsAFinding(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	w := New(slog.New(slog.DiscardHandler), fixedClock(&now),
		lane("ledger", time.Hour, func() (Reading, error) { return Reading{}, errors.New("원장 손상") }))

	f := w.Check(context.Background())
	if len(f) != 1 || f[0].Err == nil {
		t.Fatalf("findings = %v, want an unreadable-lane finding", f)
	}
	if got := f[0].String(); got == "" || !contains(got, "읽을 수 없음") {
		t.Errorf("finding text = %q", got)
	}
}

// Construction seeds every lane's clock — otherwise the watch's own startup
// would report every lane as silent since the zero time.
func TestStartupDoesNotReportEveryLane(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	w := New(slog.New(slog.DiscardHandler), fixedClock(&now),
		lane("a", time.Hour, func() (Reading, error) { return Reading{}, nil }),
		lane("b", time.Hour, func() (Reading, error) { return Reading{}, nil }))

	if f := w.Check(context.Background()); len(f) != 0 {
		t.Fatalf("startup check reported %v, want none", f)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
