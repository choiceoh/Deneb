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
	// Restarts counts intervals dropped because the engine's counters moved
	// backwards. Those intervals are not in the totals above.
	Restarts int `json:"restarts"`
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

	Days []EngineDay `json:"days"`

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
				for _, d := range store.Days(engineHistoryDays) {
					out.Days = append(out.Days, engineDayFrom(d))
				}
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

func engineDayFrom(d enginespeed.DayStat) EngineDay {
	r := d.Rates()
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
		Restarts:             d.Restarts,
	}
}
