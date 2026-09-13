package enginespeed

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func at(day int, hour int) time.Time {
	return time.Date(2026, 9, day, hour, 0, 0, 0, time.Local)
}

func counters(ttft, queue, tpotSum, tpotCount, e2e, busy, prompt, gen float64, running, waiting int) observe.EngineCounters {
	return observe.EngineCounters{
		Model:            "glm-5.3-flash",
		TTFTSeconds:      ttft,
		TTFTCount:        10,
		QueueSeconds:     queue,
		TPOTSeconds:      tpotSum,
		TPOTCount:        tpotCount,
		E2ESeconds:       e2e,
		E2ECount:         10,
		BusySeconds:      busy,
		PromptTokens:     prompt,
		GenerationTokens: gen,
		RunningRequests:  float64(running),
		WaitingRequests:  float64(waiting),
	}
}

// A day is the sum of its intervals, and the first scrape only opens the
// baseline — there is no interval behind it to attribute.
func TestStore_FirstScrapeIsABaselineAndLaterOnesAccumulate(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "engine-speed.json"))

	s.Observe("e", at(13, 1), PollInterval, counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0))
	days := s.Days(0)
	if len(days) != 1 || days[0].Rates().Measured() {
		t.Fatalf("a lone baseline must measure nothing: %+v", days)
	}

	// Two intervals of identical work must read as one day of both.
	s.Observe("e", at(13, 2), PollInterval, counters(2, 0, 10, 500, 30, 15, 4000, 500, 1, 0))
	s.Observe("e", at(13, 3), PollInterval, counters(4, 0, 20, 1000, 60, 30, 8000, 1000, 1, 0))

	got := s.Days(0)[0]
	if got.Delta.PromptTokens != 8000 || got.Delta.GenerationTokens != 1000 {
		t.Fatalf("day totals = %+v, want the whole day's growth", got.Delta)
	}
	if math.Abs(got.Rates().DecodeTokensPerSec-50) > 0.01 {
		t.Errorf("decode = %.2f, want 50", got.Rates().DecodeTokensPerSec)
	}
	if math.Abs(got.Rates().ConcurrencyWhileBusy-2) > 0.01 {
		t.Errorf("concurrency = %.2f, want 2", got.Rates().ConcurrencyWhileBusy)
	}
}

// An engine restart costs the interval it falls in, not the day and not the
// series: the reset reading becomes the new baseline and the day keeps going.
func TestStore_ARestartCostsOneIntervalAndIsCounted(t *testing.T) {
	s := NewStore("")

	s.Observe("e", at(13, 1), PollInterval, counters(10, 0, 40, 2000, 100, 50, 20000, 2000, 0, 0))
	// The engine came back: every counter is smaller than before.
	s.Observe("e", at(13, 2), PollInterval, counters(1, 0, 2, 100, 5, 2, 500, 100, 0, 0))
	// And then served more work on the fresh counters.
	s.Observe("e", at(13, 3), PollInterval, counters(3, 0, 6, 300, 15, 6, 1500, 300, 0, 0))

	got := s.Days(0)[0]
	if got.Restarts != 1 {
		t.Errorf("Restarts = %d, want 1", got.Restarts)
	}
	// Only the post-restart interval counted: 1500-500 prompt tokens.
	if got.Delta.PromptTokens != 1000 {
		t.Errorf("PromptTokens = %v, want 1000 — the reset interval must be dropped, not negated",
			got.Delta.PromptTokens)
	}
	if got.Delta.PromptTokens < 0 {
		t.Fatal("a reset must never produce a negative total")
	}
}

// The peak is the highest any scrape saw, and it is per day.
func TestStore_PeakConcurrencyIsPerDayAndFromTheGauges(t *testing.T) {
	s := NewStore("")

	s.Observe("e", at(13, 1), PollInterval, counters(1, 0, 1, 10, 1, 1, 100, 10, 1, 0))
	s.Observe("e", at(13, 2), PollInterval, counters(2, 0, 2, 20, 2, 2, 200, 20, 3, 2)) // 5
	s.Observe("e", at(13, 3), PollInterval, counters(3, 0, 3, 30, 3, 3, 300, 30, 2, 0))
	s.Observe("e", at(14, 1), PollInterval, counters(4, 0, 4, 40, 4, 4, 400, 40, 1, 0))

	days := s.Days(0)
	if len(days) != 2 {
		t.Fatalf("want one row per day, got %d", len(days))
	}
	if days[0].Day != "2026-09-14" || days[0].PeakConcurrency != 1 {
		t.Errorf("newest day = %s peak %d, want 2026-09-14 peak 1", days[0].Day, days[0].PeakConcurrency)
	}
	if days[1].PeakConcurrency != 5 {
		t.Errorf("peak = %d, want 5 (3 running + 2 waiting) — the day's highest, not its last",
			days[1].PeakConcurrency)
	}
	if days[1].PollIntervalSec != int(PollInterval.Seconds()) {
		t.Error("the poll cadence must be recorded, since the peak is only a floor at that cadence")
	}
}

// The history has to survive a gateway restart, or every restart would erase
// the day so far.
func TestStore_RoundTripsThroughDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-speed.json")
	s := NewStore(path)
	s.Observe("e", at(13, 1), PollInterval, counters(1, 0, 1, 10, 2, 1, 100, 10, 4, 0))
	s.Observe("e", at(13, 2), PollInterval, counters(3, 0, 3, 30, 6, 3, 300, 30, 1, 0))
	if err := s.Flush(at(13, 3)); err != nil {
		t.Fatalf("flush: %v", err)
	}

	reloaded := NewStore(path).Days(0)
	if len(reloaded) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(reloaded))
	}
	if reloaded[0].PeakConcurrency != 4 || reloaded[0].Delta.PromptTokens != 200 {
		t.Fatalf("reloaded = %+v, want peak 4 and 200 prompt tokens", reloaded[0])
	}
}

// Retention drops old days; a same-day second engine must not be cut off by a
// row count, because the window is counted in DAYS.
func TestStore_DayWindowCountsDaysNotRows(t *testing.T) {
	s := NewStore("")
	for _, ep := range []string{"a", "b"} {
		s.Observe(ep, at(13, 1), PollInterval, counters(1, 0, 1, 10, 1, 1, 100, 10, 1, 0))
		s.Observe(ep, at(14, 1), PollInterval, counters(2, 0, 2, 20, 2, 2, 200, 20, 1, 0))
	}
	got := s.Days(1)
	if len(got) != 2 {
		t.Fatalf("one day of two engines is 2 rows, got %d", len(got))
	}
	for _, r := range got {
		if r.Day != "2026-09-14" {
			t.Errorf("row from %s leaked past the 1-day window", r.Day)
		}
	}
}
