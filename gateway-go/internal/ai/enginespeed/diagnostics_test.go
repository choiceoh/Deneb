package enginespeed

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func TestDiagnosticGapsRuntimeResetAndPersistence(t *testing.T) {
	start := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	s := NewStore(filepath.Join(t.TempDir(), "speed.json"))
	c := observe.EngineCounters{Model: "m", Diagnostics: observe.EngineDiagnostics{Identity: "boot-a", Values: map[string]float64{"n": 10}}}
	s.Observe("ep", start, 15*time.Second, c)
	c.Diagnostics.Values = map[string]float64{"n": 15}
	s.Observe("ep", start.Add(15*time.Second), 15*time.Second, c)
	c.Diagnostics.Values = map[string]float64{"n": 20}
	s.Observe("ep", start.Add(30*time.Second), 15*time.Second, c)
	r := s.Diagnostics("ep")
	if len(r) != 2 || r[1].Values["n"] != 10 || r[1].Seconds != 30 {
		t.Fatalf("intervals %+v", r)
	}
	r[1].Values["n"] = 900
	if s.Diagnostics("ep")[1].Values["n"] != 10 {
		t.Fatal("snapshot shares live maps")
	}
	c.Diagnostics.Values = map[string]float64{"n": 40}
	s.Observe("ep", start.Add(3*time.Minute), 15*time.Second, c)
	c.Diagnostics.Identity = "boot-b"
	s.Observe("ep", start.Add(195*time.Second), 15*time.Second, c)
	c.Diagnostics.Values = map[string]float64{"n": 1}
	s.Observe("ep", start.Add(210*time.Second), 15*time.Second, c)
	r = s.Diagnostics("ep")
	for i, want := range []string{"sampling_gap", "runtime_changed", "counter_reset"} {
		if r[i+2].Event != want || r[i+2].Seconds != 0 {
			t.Fatalf("bad marker %+v", r[i+2])
		}
	}
	if err := s.Flush(start.Add(210 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(s.path)
	if len(reloaded.Diagnostics("ep")) != 5 {
		t.Fatal("history lost")
	}
	reloaded.Observe("ep", start.Add(225*time.Second), 15*time.Second, c)
	r = reloaded.Diagnostics("ep")
	if r[len(r)-1].Event != "baseline" {
		t.Fatal("gateway restart crossed an unknown gap")
	}
}

func TestDiagnosticIncompleteSampleBreaksInterval(t *testing.T) {
	s := NewStore("")
	start := time.Now()
	for i, truncated := range []bool{false, true, false, false} {
		c := observe.EngineCounters{Model: "m", Diagnostics: observe.EngineDiagnostics{Truncated: truncated, Values: map[string]float64{"n": float64(i)}}}
		s.Observe("ep", start.Add(time.Duration(i)*time.Minute), time.Minute, c)
	}
	rows := s.Diagnostics("ep")
	if rows[1].Event != "incomplete_sample" || rows[2].Event != "incomplete_sample" || rows[3].Values["n"] != 1 {
		t.Fatalf("bad incomplete boundary: %+v", rows)
	}
}
