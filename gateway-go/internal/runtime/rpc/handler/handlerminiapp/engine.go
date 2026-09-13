// engine.go — miniapp.engine.* RPC: the local serving engine's operational
// picture for the native app.
//
// It answers one question the app could not previously ask: is the engine
// running, how fast is it, and how much of the traffic did it actually take.
// The last part is not optional decoration. The local engine and its cloud twin
// answer under the SAME model name, so speed alone reads as if every turn ran
// locally; between 2026-09-06 and 09-13 the router substituted 4,949 times and
// nothing in the replies said so. Speed without share is a number that misleads.
package handlerminiapp

import (
	"context"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// EngineDay is one local day measured by the engine's own counters — never by
// the gateway's clock, which reports a prefill of 0 ms because the provider
// withholds response headers until the first token.
//
//deneb:wire
type EngineDay struct {
	Day   string `json:"day"`
	Model string `json:"model,omitempty"`
	// DecodeTokensPerSec and PrefillTokensPerSec are 0 when the engine served
	// nothing that day; Measured says which case this is so the app can show
	// "not measured" instead of "0 tok/s".
	Measured             bool    `json:"measured"`
	DecodeTokensPerSec   float64 `json:"decodeTokensPerSec,omitempty"`
	PrefillTokensPerSec  float64 `json:"prefillTokensPerSec,omitempty"`
	ConcurrencyWhileBusy float64 `json:"concurrencyWhileBusy,omitempty"`
	// PeakConcurrency is a floor, not a maximum: it is the highest value seen
	// by a poll every PollIntervalSec seconds, so a burst between polls is
	// invisible.
	PeakConcurrency int   `json:"peakConcurrency"`
	PollIntervalSec int   `json:"pollIntervalSec,omitempty"`
	Requests        int64 `json:"requests"`
	PromptTokens    int64 `json:"promptTokens"`
	GeneratedTokens int64 `json:"generatedTokens"`

	// Latency as a caller feels it. MeanTtftSeconds INCLUDES the queue;
	// MeanQueueSeconds is how much of it was waiting for a row rather than
	// prefilling, so a slow first token can be blamed on the right half.
	MeanTtftSeconds  float64 `json:"meanTtftSeconds,omitempty"`
	MeanQueueSeconds float64 `json:"meanQueueSeconds,omitempty"`
	MeanE2eSeconds   float64 `json:"meanE2eSeconds,omitempty"`

	// PromptCacheHitRatio is the share of prompt TOKENS the engine already had
	// and did not prefill — the difference between a 40K head costing 20
	// seconds and costing nothing.
	PromptCacheHitRatio float64 `json:"promptCacheHitRatio,omitempty"`
	CachedPromptTokens  int64   `json:"cachedPromptTokens,omitempty"`

	// BusySeconds is stepping time; ObservedSeconds is how long the sampler
	// watched (polls x interval). Their ratio is utilization, computed here so
	// the app never has to know the sampling cadence.
	BusySeconds     float64 `json:"busySeconds,omitempty"`
	ObservedSeconds float64 `json:"observedSeconds,omitempty"`
	Utilization     float64 `json:"utilization,omitempty"`

	// Restarts counts intervals dropped because the engine's counters moved
	// backwards. Those intervals are not in the totals above.
	Restarts int `json:"restarts"`
}

// EngineTotals folds the whole window into one row. Rates are recomputed from
// the summed deltas, never averaged from the daily rates — a day with four
// requests would otherwise weigh as much as a day with four hundred.
//
//deneb:wire
type EngineTotals struct {
	Days                int     `json:"days"`
	Requests            int64   `json:"requests"`
	PromptTokens        int64   `json:"promptTokens"`
	GeneratedTokens     int64   `json:"generatedTokens"`
	DecodeTokensPerSec  float64 `json:"decodeTokensPerSec,omitempty"`
	PrefillTokensPerSec float64 `json:"prefillTokensPerSec,omitempty"`
	MeanTtftSeconds     float64 `json:"meanTtftSeconds,omitempty"`
	PromptCacheHitRatio float64 `json:"promptCacheHitRatio,omitempty"`
	BusySeconds         float64 `json:"busySeconds,omitempty"`
	ObservedSeconds     float64 `json:"observedSeconds,omitempty"`
	Utilization         float64 `json:"utilization,omitempty"`
	Restarts            int     `json:"restarts"`
}

// EngineRoutingRow is one router entry's served total. Local marks the entries
// that point at a local engine.
//
//deneb:wire
type EngineRoutingRow struct {
	Model        string `json:"model"`
	Local        bool   `json:"local"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
}

// EngineStatusResult is the miniapp.engine.status response.
//
//deneb:wire
type EngineStatusResult struct {
	// Configured is false when no local engine endpoint is set at all — a
	// different thing from one that is set and unreachable.
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint,omitempty"`
	// Reachable is a live probe at request time, so the app shows the engine's
	// state now rather than whenever it was last polled.
	Reachable       bool   `json:"reachable"`
	Model           string `json:"model,omitempty"`
	RunningRequests int    `json:"runningRequests"`
	WaitingRequests int    `json:"waitingRequests"`

	Days  []EngineDay  `json:"days"`
	Total EngineTotals `json:"total"`

	// RouterAvailable distinguishes "the router said nothing ran locally" from
	// "we could not ask the router". In a postmortem those look identical.
	RouterAvailable bool               `json:"routerAvailable"`
	RouterWindow    string             `json:"routerWindow,omitempty"`
	LocalRequests   int64              `json:"localRequests"`
	RemoteRequests  int64              `json:"remoteRequests"`
	Routing         []EngineRoutingRow `json:"routing"`
}

// EngineDeps holds the lazy dependencies for the engine RPC.
type EngineDeps struct {
	// Speed resolves the day history. Lazy: the maintenance suite that owns it
	// is built after early RPC registration.
	Speed func() *enginespeed.Store
	// Endpoints lists the configured engine /metrics URLs.
	Endpoints func() []string
	// RouterMeter resolves the router's base URL, gate token and the set of its
	// entries that target a local engine.
	RouterMeter func() (baseURL, token string, localModels map[string]bool)
}

// EngineMethods returns the Mini App engine RPC handlers.
func EngineMethods(deps EngineDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.engine.status": engineStatus(deps),
	}
}

func engineStatus(deps EngineDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.engine.status requires client identity context").Response(req.ID)
		}
		out := EngineStatusResult{Days: []EngineDay{}, Routing: []EngineRoutingRow{}}

		var endpoint string
		if deps.Endpoints != nil {
			if eps := deps.Endpoints(); len(eps) > 0 {
				endpoint = eps[0]
			}
		}
		if endpoint != "" {
			out.Configured = true
			out.Endpoint = endpoint
			if c, ok := observe.FetchEngineCounters(ctx, endpoint); ok {
				out.Reachable = true
				out.Model = c.Model
				out.RunningRequests = int(c.RunningRequests)
				out.WaitingRequests = int(c.WaitingRequests)
			}
		}

		if deps.Speed != nil {
			if store := deps.Speed(); store != nil {
				rows := store.Days(engineHistoryDays)
				var summed observe.EngineDelta
				var observed float64
				for _, d := range rows {
					out.Days = append(out.Days, engineDayFrom(d))
					summed = summed.Add(d.Delta)
					observed += observedSeconds(d)
					out.Total.Restarts += d.Restarts
				}
				out.Total = engineTotalsFrom(out.Total, len(rows), observed, summed)
			}
		}

		if deps.RouterMeter != nil {
			baseURL, token, local := deps.RouterMeter()
			if usage, ok := observe.FetchRouterUsage(ctx, baseURL, token); ok {
				out.RouterAvailable = true
				out.RouterWindow = usage.Window
				out.LocalRequests, out.RemoteRequests = usage.LocalShare(local)
				for _, m := range usage.Models {
					out.Routing = append(out.Routing, EngineRoutingRow{
						Model: m.Model, Local: local[m.Model], Requests: m.Requests,
						InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
					})
				}
				sort.SliceStable(out.Routing, func(i, j int) bool { return out.Routing[i].Requests > out.Routing[j].Requests })
			}
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

// engineHistoryDays is how much of the day history the panel shows. The store
// retains 30; a phone screen reads a week.
const engineHistoryDays = 7

// observedSeconds is how long the sampler actually watched a day: one poll
// covers one interval. It is the honest denominator for utilization — wall
// seconds in a day would count the hours the gateway was not even running.
func observedSeconds(d enginespeed.DayStat) float64 {
	if d.PollIntervalSec <= 0 || d.Polls <= 0 {
		return 0
	}
	return float64(d.Polls) * float64(d.PollIntervalSec)
}

func engineTotalsFrom(base EngineTotals, days int, observed float64, summed observe.EngineDelta) EngineTotals {
	r := summed.Rates()
	base.Days = days
	base.Requests = r.Requests
	base.PromptTokens = r.PromptTokens
	base.GeneratedTokens = r.GeneratedTokens
	base.DecodeTokensPerSec = r.DecodeTokensPerSec
	base.PrefillTokensPerSec = r.PrefillTokensPerSec
	base.MeanTtftSeconds = r.MeanTTFTSeconds
	base.PromptCacheHitRatio = r.PromptCacheHitRatio
	base.BusySeconds = r.BusySeconds
	base.ObservedSeconds = observed
	if observed > 0 {
		base.Utilization = r.BusySeconds / observed
	}
	return base
}

func engineDayFrom(d enginespeed.DayStat) EngineDay {
	r := d.Rates()
	observed := observedSeconds(d)
	util := 0.0
	if observed > 0 {
		util = r.BusySeconds / observed
	}
	return EngineDay{
		Day:                  d.Day,
		Model:                d.Model,
		Measured:             r.Measured(),
		DecodeTokensPerSec:   r.DecodeTokensPerSec,
		PrefillTokensPerSec:  r.PrefillTokensPerSec,
		ConcurrencyWhileBusy: r.ConcurrencyWhileBusy,
		PeakConcurrency:      d.PeakConcurrency,
		PollIntervalSec:      d.PollIntervalSec,
		Requests:             int64(r.Requests),
		PromptTokens:         int64(r.PromptTokens),
		GeneratedTokens:      int64(r.GeneratedTokens),
		MeanTtftSeconds:      r.MeanTTFTSeconds,
		MeanQueueSeconds:     r.MeanQueueSeconds,
		MeanE2eSeconds:       r.MeanE2ESeconds,
		PromptCacheHitRatio:  r.PromptCacheHitRatio,
		CachedPromptTokens:   r.CachedPromptTokens,
		BusySeconds:          r.BusySeconds,
		ObservedSeconds:      observed,
		Utilization:          util,
		Restarts:             d.Restarts,
	}
}
