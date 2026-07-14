package wiki

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestApplyDreamPartialBackpressure_HoldsTwoCyclesThenClears(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wd := &WikiDreamer{logger: logger}
	prior := map[string]diaryFileState{"memory/2026-07-11.md": {Offset: 10}}
	cycle := &dreamCycle{
		partial: true,
		scan: &diaryScanResult{
			State: diaryProcessState{
				Files: map[string]diaryFileState{"memory/2026-07-11.md": {Offset: 90}},
			},
			PriorFiles: prior,
		},
	}

	if held := wd.applyDreamPartialBackpressure(cycle); !held {
		t.Fatal("first partial synthesis must hold cursors")
	}
	if cycle.scan.State.PartialStreak != 1 || cycle.scan.State.Files["memory/2026-07-11.md"].Offset != 10 {
		t.Fatalf("first hold state = %#v, want streak 1 and restored offset 10", cycle.scan.State)
	}

	if held := wd.applyDreamPartialBackpressure(cycle); !held {
		t.Fatal("second partial synthesis must still hold cursors")
	}
	if cycle.scan.State.PartialStreak != 2 {
		t.Fatalf("second hold streak = %d, want 2", cycle.scan.State.PartialStreak)
	}

	if held := wd.applyDreamPartialBackpressure(cycle); held {
		t.Fatal("third partial synthesis must advance past deterministic damage")
	}
	if cycle.scan.State.PartialStreak != 0 {
		t.Fatalf("third partial streak = %d, want reset to 0", cycle.scan.State.PartialStreak)
	}
}

func TestApplyDreamPartialBackpressure_MemoryOnlyCyclePreservesCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wd := &WikiDreamer{logger: logger}
	cycle := &dreamCycle{
		partial: true,
		scan: &diaryScanResult{
			State: diaryProcessState{
				Files: map[string]diaryFileState{"existing": {Offset: 7}},
			},
		},
	}

	if held := wd.applyDreamPartialBackpressure(cycle); !held {
		t.Fatal("MEMORY.md-only partial cycle must hold its high-water cursor")
	}
	if cycle.scan.State.Files["existing"].Offset != 7 {
		t.Fatal("holding without PriorFiles must not erase the current diary ledger")
	}

	cycle.partial = false
	if held := wd.applyDreamPartialBackpressure(cycle); held {
		t.Fatal("complete synthesis must not hold cursors")
	}
	if cycle.scan.State.PartialStreak != 0 {
		t.Fatalf("complete synthesis streak = %d, want 0", cycle.scan.State.PartialStreak)
	}
}

func TestDreamProgressCursor_ReturnsHeldOrLatestDateOrNow(t *testing.T) {
	now := time.Date(2026, 7, 12, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name   string
		scan   *diaryScanResult
		held   bool
		wanted string
	}{
		{
			name:   "held offsets override latest date",
			scan:   &diaryScanResult{LatestDate: "2026-07-11"},
			held:   true,
			wanted: "",
		},
		{
			name:   "completed diary batch uses latest date",
			scan:   &diaryScanResult{LatestDate: "2026-07-11"},
			wanted: "2026-07-11",
		},
		{
			name:   "memory-only batch uses current date",
			scan:   &diaryScanResult{},
			wanted: "2026-07-12",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dreamProgressCursor(test.scan, test.held, now); got != test.wanted {
				t.Fatalf("dreamProgressCursor() = %q, want %q", got, test.wanted)
			}
		})
	}
}
