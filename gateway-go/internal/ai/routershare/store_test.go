package routershare

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func usage(window string, rows ...observe.RouterModelUsage) observe.RouterUsage {
	return observe.RouterUsage{Window: window, Models: rows}
}

// The meter is month-cumulative; the store files its GROWTH by local day. The
// first reading is a baseline, a new month re-baselines, and a counter that
// moved backwards (router state reset) does too.
func TestObserveFilesGrowthByDay(t *testing.T) {
	loc := time.FixedZone("KST", 9*3600)
	s := NewStore("")
	d1 := time.Date(2026, 9, 15, 23, 50, 0, 0, loc)

	s.Observe(d1, usage("2026-09", observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 100, InputTokens: 1000}))
	if got := s.Days(0); len(got) != 0 {
		t.Fatalf("the first reading is a baseline only: %+v", got)
	}
	s.Observe(d1.Add(time.Minute), usage("2026-09",
		observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 104, InputTokens: 1400, OutputTokens: 50},
		observe.RouterModelUsage{Model: "k3", Requests: 7})) // first sight of k3: baseline
	s.Observe(d1.Add(11*time.Minute), usage("2026-09", // now the 16th, 00:01
		observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 110, InputTokens: 1400, OutputTokens: 50},
		observe.RouterModelUsage{Model: "k3", Requests: 9}))

	got := s.Days(0)
	if len(got) != 3 {
		t.Fatalf("rows = %+v", got)
	}
	if got[0].Day != "2026-09-16" || got[0].Model != "glm-5.3-flash" || got[0].Requests != 6 {
		t.Errorf("16th glm = %+v, want 6 requests", got[0])
	}
	if got[1].Day != "2026-09-16" || got[1].Model != "k3" || got[1].Requests != 2 {
		t.Errorf("16th k3 = %+v, want 2 requests", got[1])
	}
	if got[2].Day != "2026-09-15" || got[2].Requests != 4 || got[2].InputTokens != 400 || got[2].OutputTokens != 50 || got[2].Polls != 1 {
		t.Errorf("15th glm = %+v", got[2])
	}

	// Month rollover (still 23:5x on the 16th): the router's counters start
	// over — baseline only, then growth files under the 16th again.
	d2 := d1.Add(24 * time.Hour)
	s.Observe(d2, usage("2026-10", observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 3}))
	s.Observe(d2.Add(2*time.Minute), usage("2026-10", observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 5}))
	// A counter moving backwards inside a window is a reset — baseline only.
	s.Observe(d2.Add(4*time.Minute), usage("2026-10", observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 1}))
	s.Observe(d2.Add(6*time.Minute), usage("2026-10", observe.RouterModelUsage{Model: "glm-5.3-flash", Requests: 4}))
	rows := s.Days(1)
	if len(rows) != 2 || rows[0].Day != "2026-09-16" || rows[0].Requests != 6+2+3 {
		t.Errorf("after rollover and reset the 16th must hold 6 (Sept) + 2 + 3 (Oct deltas): %+v", rows)
	}
}

func TestDaysLimitsToDistinctDaysAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router-share.json")
	s := NewStore(path)
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s.Observe(base, usage("2026-09", observe.RouterModelUsage{Model: "a", Requests: 0}))
	for day := 1; day <= 4; day++ {
		s.Observe(base.AddDate(0, 0, day), usage("2026-09", observe.RouterModelUsage{Model: "a", Requests: int64(day * 10)}))
	}
	if got := s.Days(2); len(got) != 2 || got[0].Day != "2026-09-14" || got[1].Day != "2026-09-13" {
		t.Errorf("Days(2) = %+v", got)
	}
	if err := s.Flush(base.AddDate(0, 0, 4)); err != nil {
		t.Fatal(err)
	}
	if got := NewStore(path).Days(0); len(got) != 4 || got[3].Day != "2026-09-11" || got[3].Requests != 10 {
		t.Errorf("reloaded = %+v", got)
	}
}

func TestNewTaskNeedsAStoreAndAMeter(t *testing.T) {
	if NewTask(nil, func() (string, string, map[string]bool) { return "", "", nil }, nil) != nil {
		t.Error("no store → no task")
	}
	if NewTask(NewStore(""), nil, nil) != nil {
		t.Error("no meter → no task")
	}
	task := NewTask(NewStore(""), func() (string, string, map[string]bool) { return "", "", nil }, nil)
	if task == nil || task.Name() != "router-share" || task.Interval() != PollInterval {
		t.Fatalf("task = %+v", task)
	}
	// An unresolvable router is a no-op, not an error.
	if err := task.Run(t.Context()); err != nil {
		t.Errorf("Run = %v", err)
	}
}
