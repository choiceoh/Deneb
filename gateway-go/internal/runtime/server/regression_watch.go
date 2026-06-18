package server

import (
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/regressionwatch"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
)

// regressionWindow is the telemetry look-back each watch cycle samples.
const regressionWindow = 24 * time.Hour

// regressionSources assembles the regression-watch signal adapters. It lives in
// the server package (not ai/regressionwatch) because the observe-log adapter
// must reach runtime/observe, and the regression core must not import runtime
// (layering). agentlog + model-health are layer-safe and stay in the core.
func (s *Server) regressionSources() []regressionwatch.SignalSource {
	srcs := []regressionwatch.SignalSource{
		regressionwatch.AgentLogSource{Logs: s.agentLogWriter, Window: regressionWindow},
		regressionwatch.HealthSource{Registry: s.modelRegistry},
	}
	if s.logCapture != nil {
		srcs = append(srcs, observeLogSource{ring: s.logCapture.Ring(), window: regressionWindow})
	}
	return srcs
}

// observeLogSource adapts the in-memory log ring into a regression signal: a
// spike in Error-level lines over the recent window. Delivery failures always
// log at Error (per the logging rules), so they are absorbed here and need no
// dedicated counter. The ring is fixed-size, so a very busy window can shadow
// older lines — fine for "did errors spike recently" but not an exact 24h count.
type observeLogSource struct {
	ring   *observe.Ring
	window time.Duration
}

func (s observeLogSource) Name() string { return "observe-log" }

func (s observeLogSource) Sample() []regressionwatch.Signal {
	if s.ring == nil {
		return nil
	}
	since := time.Now().Add(-s.window).UnixMilli()
	// Cap the scan at the ring size so the whole retained window is counted, not
	// just the default query page. The ring is bounded, so this stays cheap.
	errLines := s.ring.Query(observe.QueryOpts{
		MinLevel: slog.LevelError,
		SinceMs:  since,
		Limit:    s.ring.Cap(),
	})
	return []regressionwatch.Signal{{
		Key:         "observe.error_lines",
		Value:       float64(len(errLines)),
		Sample:      1,
		HigherWorse: true,
		Kind:        regressionwatch.KindCount,
		HardFloor:   10, // a sudden +10 error lines over baseline is worth a look
	}}
}
