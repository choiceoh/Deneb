package enginespeed

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func servedAs(model string, c observe.EngineCounters) observe.EngineCounters {
	c.Model = model
	return c
}

// One endpoint served GLM-5.3 and then Qwen3.8 on the same day. Each model
// keeps its own row, and the interval that spans the switch belongs to neither
// — even when the new process's counters happen to exceed the old one's, which
// is exactly when the counters alone would have filed it under the new model.
func TestStore_AModelSwitchSplitsTheDayAndDropsTheSpanningInterval(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "engine-speed.json"))

	s.Observe("e", at(19, 1), PollInterval, servedAs("glm-5.3-flash", counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)))
	s.Observe("e", at(19, 2), PollInterval, servedAs("glm-5.3-flash", counters(2, 0, 10, 500, 30, 15, 4000, 500, 1, 0)))
	// Higher on every series than GLM's last reading: a delta would be "valid".
	s.Observe("e", at(19, 3), PollInterval, servedAs("qwen3.8-flash-next", counters(9, 0, 90, 900, 90, 90, 9000, 900, 0, 0)))
	s.Observe("e", at(19, 4), PollInterval, servedAs("qwen3.8-flash-next", counters(10, 0, 100, 1400, 120, 100, 11000, 1400, 1, 0)))

	days := s.Days(0)
	if len(days) != 2 {
		t.Fatalf("rows = %d, want one per model: %+v", len(days), days)
	}
	glm, qwen := days[0], days[1] // same day: model order
	if glm.Model != "glm-5.3-flash" || qwen.Model != "qwen3.8-flash-next" {
		t.Fatalf("row order = %q, %q", glm.Model, qwen.Model)
	}
	if glm.Delta.PromptTokens != 4000 || glm.Restarts != 0 {
		t.Errorf("GLM row = %+v, want only its own interval", glm.Delta)
	}
	if qwen.Delta.PromptTokens != 2000 || qwen.Delta.GenerationTokens != 500 {
		t.Errorf("Qwen row = %+v, want only the interval after the switch", qwen.Delta)
	}
	if qwen.Restarts != 1 {
		t.Errorf("Qwen restarts = %d, want the switch counted once", qwen.Restarts)
	}
}

// A handover answers /v1/models with an empty catalog while the same process
// keeps counting. That scrape continues the last model's series: no "" row,
// no restart, and no runtime change in the diagnostics.
func TestStore_AnUnnamedScrapeContinuesTheLastModel(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "engine-speed.json"))

	s.Observe("e", at(19, 1), PollInterval, servedAs("glm-5.3-flash", counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)))
	s.Observe("e", at(19, 2), PollInterval, servedAs("", counters(2, 0, 10, 500, 30, 15, 4000, 500, 1, 0)))

	days := s.Days(0)
	if len(days) != 1 || days[0].Model != "glm-5.3-flash" {
		t.Fatalf("rows = %+v, want the one GLM row", days)
	}
	if days[0].Delta.PromptTokens != 4000 || days[0].Restarts != 0 {
		t.Errorf("GLM row = %+v restarts %d, want the interval kept", days[0].Delta, days[0].Restarts)
	}
	for _, row := range s.Diagnostics("e") {
		if row.Event == "runtime_changed" {
			t.Errorf("an unnamed scrape recorded a runtime change: %+v", row)
		}
	}
}

// The glance tile names the engine's model without probing it, so the store
// has to know which model it saw last — live, and across a gateway restart.
func TestStore_CurrentModelFollowsTheLastScrapeAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-speed.json")
	s := NewStore(path)
	if got := s.CurrentModel("e"); got != "" {
		t.Fatalf("empty store names %q", got)
	}
	s.Observe("e", at(19, 10), PollInterval, servedAs("qwen3.8-flash-next", counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)))
	s.Observe("e", at(19, 11), PollInterval, servedAs("glm-5.3-flash", counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)))
	if got := s.CurrentModel("e"); got != "glm-5.3-flash" {
		t.Fatalf("live current = %q, want the last scrape's", got)
	}
	if err := s.Flush(at(19, 12)); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(path)
	if got := reloaded.CurrentModel("e"); got != "glm-5.3-flash" {
		t.Errorf("after restart = %q, want the row seen last", got)
	}
	if got := reloaded.CurrentModel("other"); got != "" {
		t.Errorf("an endpoint never scraped names %q", got)
	}
}

// History written before the model was part of the key has one row per day
// and no LastSeenMs. It loads under its own model, and the later day decides
// the current one.
func TestStore_RowsFromBeforeTheModelKeyLoadUnderTheirModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-speed.json")
	legacy := `{"days":[
		{"day":"2026-09-18","endpoint":"e","model":"glm-5.3-flash","delta":{},"polls":10,"restarts":0,"peakConcurrency":1},
		{"day":"2026-09-19","endpoint":"e","model":"qwen3.8-flash-next","delta":{},"polls":4,"restarts":1,"peakConcurrency":1}
	]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	days := s.Days(0)
	if len(days) != 2 || days[0].Model != "qwen3.8-flash-next" || days[1].Model != "glm-5.3-flash" {
		t.Fatalf("legacy rows = %+v", days)
	}
	if got := s.CurrentModel("e"); got != "qwen3.8-flash-next" {
		t.Errorf("current = %q, want the later day's model", got)
	}

	// A new scrape of the other model the same day opens its own row beside it.
	s.Observe("e", time.Date(2026, 9, 19, 23, 0, 0, 0, time.Local), PollInterval, servedAs("glm-5.3-flash", counters(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)))
	if n := len(s.Days(1)); n != 2 {
		t.Errorf("rows on 09-19 = %d, want the legacy Qwen row and a new GLM row", n)
	}
}
