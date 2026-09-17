package enginespeed

import (
	"maps"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// DiagnosticInterval holds one minute of counter deltas within one runtime.
// Gaps and resets are zero-duration markers; neither is recorded as zero speed.
type DiagnosticInterval struct {
	Endpoint string             `json:"endpoint"`
	Model    string             `json:"model"`
	Identity string             `json:"identity"`
	SinceMs  int64              `json:"sinceMs"`
	UntilMs  int64              `json:"untilMs"`
	Seconds  float64            `json:"seconds"`
	Values   map[string]float64 `json:"values,omitempty"`
	Event    string             `json:"event,omitempty"`
}

type diagnosticPrevious struct {
	at time.Time
	c  observe.EngineCounters
}

func (s *Store) observeDiagnosticsLocked(endpoint string, at time.Time, cadence time.Duration, c observe.EngineCounters) {
	if s.diagnosticPrevious == nil {
		s.diagnosticPrevious = map[string]diagnosticPrevious{}
	}
	prev, exists := s.diagnosticPrevious[endpoint]
	s.diagnosticPrevious[endpoint] = diagnosticPrevious{at: at, c: c}
	id := c.Model + " " + c.Diagnostics.Identity
	row := DiagnosticInterval{Endpoint: endpoint, Model: c.Model, Identity: id, SinceMs: at.UnixMilli(), UntilMs: at.UnixMilli()}
	switch {
	case c.Diagnostics.Truncated || prev.c.Diagnostics.Truncated:
		row.Event = "incomplete_sample"
	case !exists:
		row.Event = "baseline"
	case !at.After(prev.at):
		return
	case prev.c.Model != c.Model || prev.c.Diagnostics.Identity != c.Diagnostics.Identity:
		row.Event = "runtime_changed"
	case at.Sub(prev.at) > 3*cadence:
		row.Event = "sampling_gap"
	default:
		row.Values = map[string]float64{}
		for key, value := range c.Diagnostics.Values {
			if before, ok := prev.c.Diagnostics.Values[key]; ok {
				if value < before {
					row.Event = "counter_reset"
					break
				}
				row.Values[key] = value - before
			}
		}
		if row.Event == "" {
			row.SinceMs, row.Seconds = prev.at.UnixMilli(), at.Sub(prev.at).Seconds()
		} else {
			row.Values = nil
		}
	}
	// Aggregate only adjacent samples of the same endpoint, runtime and minute.
	if n := len(s.diagnostics); n > 0 && row.Event == "" {
		last := &s.diagnostics[n-1]
		if last.Endpoint == endpoint && last.Identity == id && last.Event == "" &&
			last.UntilMs == row.SinceMs && last.UntilMs/60000 == row.UntilMs/60000 {
			last.UntilMs, last.Seconds = row.UntilMs, last.Seconds+row.Seconds
			for k, v := range row.Values {
				last.Values[k] += v
			}
			s.pruneDiagnosticsLocked(at)
			return
		}
	}
	s.diagnostics = append(s.diagnostics, row)
	s.pruneDiagnosticsLocked(at)
}

func (s *Store) pruneDiagnosticsLocked(now time.Time) {
	cutoff := now.Add(-24 * time.Hour).UnixMilli()
	kept := s.diagnostics[:0]
	for _, row := range s.diagnostics {
		if row.UntilMs >= cutoff {
			kept = append(kept, row)
		}
	}
	s.diagnostics = kept
	// Secondary bound for multiple endpoints and repeated restarts.
	if len(s.diagnostics) > 12000 {
		s.diagnostics = s.diagnostics[len(s.diagnostics)-12000:]
	}
}

func copyDiagnosticIntervals(rows []DiagnosticInterval) []DiagnosticInterval {
	out := make([]DiagnosticInterval, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Values = maps.Clone(row.Values)
	}
	return out
}

// Diagnostics snapshots history without exposing maps that the sampler mutates.
func (s *Store) Diagnostics(endpoint string) []DiagnosticInterval {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []DiagnosticInterval
	for _, row := range s.diagnostics {
		if row.Endpoint == endpoint {
			rows = append(rows, row)
		}
	}
	return copyDiagnosticIntervals(rows)
}
