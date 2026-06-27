package regressionwatch

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// --- agent-log source --------------------------------------------------------

// agentlogStats is the slice of agentlog.Writer this source needs (test seam).
type agentlogStats interface {
	AggregateByModel(sinceMs int64) []agentlog.ModelStat
}

// AgentLogSource derives per-model behavioral rates from the last Window of
// agent logs: error rate, tool-error rate, timeout rate, output-ceiling rate,
// and P95 latency. These are the richest regression signals — they reflect what
// real traffic actually experienced, attributed to the requested model.
type AgentLogSource struct {
	Logs   agentlogStats
	Window time.Duration
}

func (s AgentLogSource) Name() string { return "agentlog" }

func (s AgentLogSource) Sample() []Signal {
	if s.Logs == nil {
		return nil
	}
	window := s.Window
	if window == 0 {
		window = 24 * time.Hour
	}
	since := time.Now().Add(-window).UnixMilli()
	var out []Signal
	for _, m := range s.Logs.AggregateByModel(since) {
		total := m.Runs + m.Errors
		if total == 0 {
			continue
		}
		out = append(
			out,
			Signal{Key: "agentlog.error_rate", Scope: m.Model, Value: rate(m.Errors, total), Sample: total, HigherWorse: true, Kind: KindRate},
			Signal{Key: "agentlog.timeout_rate", Scope: m.Model, Value: rate(m.TimeoutRuns, m.Runs), Sample: m.Runs, HigherWorse: true, Kind: KindRate},
			Signal{Key: "agentlog.max_tokens_rate", Scope: m.Model, Value: rate(m.MaxTokensRecoveries, m.Runs), Sample: m.Runs, HigherWorse: true, Kind: KindRate},
			Signal{Key: "agentlog.p95_ms", Scope: m.Model, Value: float64(m.P95Ms), Sample: m.Runs, HigherWorse: true, Kind: KindScalar},
		)
		if m.ToolCalls > 0 {
			out = append(out, Signal{
				Key: "agentlog.tool_error_rate", Scope: m.Model,
				Value: rate(m.ToolErrors, m.ToolCalls), Sample: m.ToolCalls, HigherWorse: true, Kind: KindRate,
			})
		}
	}
	return out
}

// --- model-health source -----------------------------------------------------

// healthRegistry is the slice of modelrole.Registry this source needs.
type healthRegistry interface {
	ConfiguredModels() map[modelrole.Role]modelrole.ModelConfig
	ModelUnhealthy(model string) bool
}

// HealthSource flags models whose circuit breaker is currently open. A model
// flipping unhealthy is an immediate, unambiguous regression — it stopped
// answering — so it is a KindCount signal (0 healthy, 1 open) with HardFloor 1:
// any flip from a healthy baseline trips it.
type HealthSource struct {
	Registry healthRegistry
}

func (s HealthSource) Name() string { return "model-health" }

func (s HealthSource) Sample() []Signal {
	if s.Registry == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []Signal
	for _, cfg := range s.Registry.ConfiguredModels() {
		if cfg.Model == "" || seen[cfg.Model] {
			continue
		}
		seen[cfg.Model] = true
		v := 0.0
		if s.Registry.ModelUnhealthy(cfg.Model) {
			v = 1.0
		}
		out = append(out, Signal{
			Key: "health.unhealthy", Scope: cfg.Model,
			Value: v, Sample: 1, HigherWorse: true, Kind: KindCount, HardFloor: 1,
		})
	}
	return out
}

func rate(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}
