package observeops

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func day(d string, delta observe.EngineDelta, peak, polls int) enginespeed.DayStat {
	return enginespeed.DayStat{
		Day: d, Endpoint: "http://engine/metrics", Model: "glm-5.3-flash",
		Delta: delta, PeakConcurrency: peak, Polls: polls, PollIntervalSec: 15,
	}
}

// The reader needs all four numbers, newest day first, and the caveats that
// keep each from being quoted as something it is not.
func TestFormatEngineSpeed_ShowsTheDaysNumbersAndTheirLimits(t *testing.T) {
	out := formatEngineSpeed([]enginespeed.DayStat{
		day("2026-09-14", observe.EngineDelta{
			TTFTSeconds: 4, QueueSeconds: 1, TPOTSeconds: 20, TPOTCount: 1000,
			E2ESeconds: 60, BusySeconds: 30, Requests: 10,
			PromptTokens: 8000, GenerationTokens: 1000,
		}, 5, 900),
		day("2026-09-13", observe.EngineDelta{}, 0, 12),
	})

	for _, want := range []string{
		"2026-09-14", "decode 50.0 tok/s", "prefill 2666.7 tok/s",
		"2.00 while busy", "5 peak observed",
		"10 requests",
		"15s poll",   // the peak's cadence, so it reads as a floor
		"queue wait", // prefill's caveat
		"idle time",  // concurrency's caveat
		"Cloud models are absent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Newest first.
	if strings.Index(out, "2026-09-14") > strings.Index(out, "2026-09-13") {
		t.Errorf("days must read newest first:\n%s", out)
	}
}

// A day the engine served nothing in must say so. Rendering it as 0 tok/s
// would make an idle day look like a slow one.
func TestFormatEngineSpeed_IdleDayIsNamedNotScoredZero(t *testing.T) {
	out := formatEngineSpeed([]enginespeed.DayStat{day("2026-09-13", observe.EngineDelta{}, 0, 40)})
	if !strings.Contains(out, "not measured") || !strings.Contains(out, "40 polls") {
		t.Errorf("an idle day must be named with its poll count:\n%s", out)
	}
	if strings.Contains(out, "tok/s") {
		t.Errorf("an idle day must not render a rate:\n%s", out)
	}
}

// A restart means some of the day is missing from the totals; the reader is
// told rather than shown a quietly short day.
func TestFormatEngineSpeed_DisclosesDiscardedRestartIntervals(t *testing.T) {
	d := day("2026-09-14", observe.EngineDelta{
		TPOTSeconds: 10, TPOTCount: 500, E2ESeconds: 20, BusySeconds: 10, Requests: 4,
	}, 2, 100)
	d.Restarts = 2
	out := formatEngineSpeed([]enginespeed.DayStat{d})
	if !strings.Contains(out, "2 engine restart") || !strings.Contains(out, "not in the totals") {
		t.Errorf("discarded intervals must be disclosed:\n%s", out)
	}
}

func TestFormatEngineSpeed_EmptyHistorySaysSo(t *testing.T) {
	if out := formatEngineSpeed(nil); !strings.Contains(out, "no samples yet") {
		t.Errorf("empty = %q", out)
	}
}
