// engine.go — miniapp.engine.* RPC: the local serving engine's operational
// picture for the native app.
//
// It answers, in this order: is the engine serving my turns right now (the
// gateway's own routing verdict, not a probe made for the screen), how much
// of today it was gone, how fast it is, and how much of the traffic actually
// ran on it. On 2026-09-16 the engine was refused 22 times for eight hours in
// total, and a panel built from its own counters showed a green dot: a live
// scrape sees one instant, and a dead process exports nothing at all.
package handlerminiapp

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/routershare"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// EngineDay is one local day measured by the engine's own counters — never by
// the gateway's clock, which reports a prefill of 0 ms because the provider
// withholds response headers until the first token. Downtime and the router's
// share ride the same row so a day reads whole.
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
	// CachePromptTokens is the measured token window's denominator; older
	// history has no token counters and is excluded from this window.
	CachePromptTokens     int64   `json:"cachePromptTokens,omitempty"`
	PromptCacheMeasured   bool    `json:"promptCacheMeasured"`
	PrefixRequestHitRatio float64 `json:"prefixRequestHitRatio,omitempty"`
	PrefixLookupRequests  int64   `json:"prefixLookupRequests,omitempty"`
	PrefixHitRequests     int64   `json:"prefixHitRequests,omitempty"`
	CachedPromptTokens    int64   `json:"cachedPromptTokens,omitempty"`

	// SpecAcceptRatio is the share of drafted tokens the model kept — the
	// drafter's whole contribution to decode speed. SpecDraftTokens is its
	// sample mass.
	SpecAcceptRatio float64 `json:"specAcceptRatio,omitempty"`
	SpecDraftTokens int64   `json:"specDraftTokens,omitempty"`

	// BusySeconds is stepping time; ObservedSeconds is how long the sampler
	// watched (polls x interval). Their ratio is utilization, computed here so
	// the app never has to know the sampling cadence.
	BusySeconds     float64 `json:"busySeconds,omitempty"`
	ObservedSeconds float64 `json:"observedSeconds,omitempty"`
	Utilization     float64 `json:"utilization,omitempty"`

	// Restarts counts intervals dropped because the engine's counters moved
	// backwards. Those intervals are not in the totals above.
	Restarts int `json:"restarts"`

	// Downtime from the liveness ledger: seconds of this day the gateway had
	// the engine's models on the fallback, and how many outages BEGAN in it.
	// LivenessTracked is false for a day before the ledger existed.
	LivenessTracked bool    `json:"livenessTracked"`
	DownSeconds     float64 `json:"downSeconds,omitempty"`
	Outages         int     `json:"outages"`

	// The router's meter for this day, differenced every minute (see
	// internal/ai/routershare) — the month meter straddles routing changes.
	// Unknown are entries the current router config no longer lists.
	RouterMetered         bool  `json:"routerMetered"`
	RouterLocalRequests   int64 `json:"routerLocalRequests"`
	RouterRemoteRequests  int64 `json:"routerRemoteRequests"`
	RouterUnknownRequests int64 `json:"routerUnknownRequests"`
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
	MeanQueueSeconds    float64 `json:"meanQueueSeconds,omitempty"`
	MeanE2eSeconds      float64 `json:"meanE2eSeconds,omitempty"`
	PromptCacheHitRatio float64 `json:"promptCacheHitRatio,omitempty"`
	CachedPromptTokens  int64   `json:"cachedPromptTokens,omitempty"`
	// CachePromptTokens is the measured token window's denominator; older
	// history has no token counters and is excluded from this window.
	CachePromptTokens     int64   `json:"cachePromptTokens,omitempty"`
	PromptCacheMeasured   bool    `json:"promptCacheMeasured"`
	PrefixRequestHitRatio float64 `json:"prefixRequestHitRatio,omitempty"`
	PrefixLookupRequests  int64   `json:"prefixLookupRequests,omitempty"`
	PrefixHitRequests     int64   `json:"prefixHitRequests,omitempty"`
	SpecAcceptRatio       float64 `json:"specAcceptRatio,omitempty"`
	SpecDraftTokens       int64   `json:"specDraftTokens,omitempty"`
	BusySeconds           float64 `json:"busySeconds,omitempty"`
	ObservedSeconds       float64 `json:"observedSeconds,omitempty"`
	Utilization           float64 `json:"utilization,omitempty"`
	Restarts              int     `json:"restarts"`

	DownSeconds float64 `json:"downSeconds,omitempty"`
	Outages     int     `json:"outages"`

	RouterLocalRequests   int64 `json:"routerLocalRequests"`
	RouterRemoteRequests  int64 `json:"routerRemoteRequests"`
	RouterUnknownRequests int64 `json:"routerUnknownRequests"`
}

// EngineRoutingRow is one router entry's served total. Local marks the entries
// that point at a local engine; Known is false for an entry the router's
// current config no longer lists (a renamed entry keeps its month counter).
//
//deneb:wire
type EngineRoutingRow struct {
	Model        string `json:"model"`
	Local        bool   `json:"local"`
	Known        bool   `json:"known"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	// The router's live state for the entry (GET /status), when it answered.
	// CircuitState "open" means the router holds traffic off this entry
	// until RetryAfterMs — the reason a share can be low while the engine is
	// up. KeyHealth is the cloud entry's last upstream-auth probe.
	CircuitState    string `json:"circuitState,omitempty"`
	CircuitFailures int    `json:"circuitFailures,omitempty"`
	RetryAfterMs    int64  `json:"retryAfterMs,omitempty"`
	KeyHealth       string `json:"keyHealth,omitempty"`
	UpstreamMissing bool   `json:"upstreamMissing,omitempty"`
}

// EngineOutage is one span in which the gateway had the engine's models on the
// fallback. UntilMs is 0 while it is still going on.
//
//deneb:wire
type EngineOutage struct {
	SinceMs     int64   `json:"sinceMs"`
	UntilMs     int64   `json:"untilMs,omitempty"`
	DurationSec float64 `json:"durationSec"`
	Reason      string  `json:"reason,omitempty"`
}

// EngineInternals is the engine's own account of its memory and cache budget
// at the moment of the call, plus what it says about the fleet lock. These
// are the numbers behind the cache hit ratio and behind an outage's cause.
//
//deneb:wire
type EngineInternals struct {
	// Published is false when the engine exports none of the st: gauges.
	Published           bool `json:"published"`
	PrefixEntries       int  `json:"prefixEntries"`
	PrefixSnapshotsFree int  `json:"prefixSnapshotsFree"`
	PrefixPinnedEntries int  `json:"prefixPinnedEntries"`
	PrefixTierEntries   int  `json:"prefixTierEntries"`
	KvBlocksTotal       int  `json:"kvBlocksTotal"`
	KvBlocksUsed        int  `json:"kvBlocksUsed"`
	KvBlocksCached      int  `json:"kvBlocksCached"`
	ConversationsParked int  `json:"conversationsParked"`

	DeviceMemoryTotalBytes    int64 `json:"deviceMemoryTotalBytes"`
	DeviceMemoryFreeBytes     int64 `json:"deviceMemoryFreeBytes"`
	DeviceMemoryReservedBytes int64 `json:"deviceMemoryReservedBytes"`
	HostMemoryAvailableBytes  int64 `json:"hostMemoryAvailableBytes"`

	HandingOver bool `json:"handingOver"`
	Quiet       bool `json:"quiet"`

	// From the engine's root status document. FleetKnown is false when it
	// reported no fleet block; Served and Steps reset with the process.
	FleetKnown      bool   `json:"fleetKnown"`
	FleetOwner      string `json:"fleetOwner,omitempty"`
	FleetDraining   string `json:"fleetDraining,omitempty"`
	FleetHandedOver string `json:"fleetHandedOver,omitempty"`
	Served          int64  `json:"served"`
	Steps           int64  `json:"steps"`
}

// EngineStatusResult is the miniapp.engine.status response.
//
//deneb:wire
type EngineStatusResult struct {
	Diagnostics EngineDiagnosticsReport `json:"diagnostics"`
	// NowMs is the gateway's clock at assembly; every relative time the app
	// shows ("down for 12 min") is measured against it, not the phone's.
	NowMs int64 `json:"nowMs"`

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

	// The gateway's routing verdict from the liveness watcher (/health every
	// five seconds, with hysteresis). This, not Reachable, decides where a
	// turn goes: while EngineDown the models in DownModels skip straight to
	// the fallback chain. LivenessTracked is false when no watcher runs.
	LivenessTracked bool     `json:"livenessTracked"`
	EngineDown      bool     `json:"engineDown"`
	DownSinceMs     int64    `json:"downSinceMs,omitempty"`
	UpSinceMs       int64    `json:"upSinceMs,omitempty"`
	DownReason      string   `json:"downReason,omitempty"`
	DownModels      []string `json:"downModels"`
	// TrackedSinceMs is the earliest moment availability is known; before it
	// the timeline is blank, not "up".
	TrackedSinceMs int64 `json:"trackedSinceMs,omitempty"`
	// Outages in the window, newest first; the first may be ongoing.
	Outages []EngineOutage `json:"outages"`

	Internals EngineInternals `json:"internals"`

	// Models the engine served in the window, current first. Days, Total and
	// Diagnostics are SelectedModel's alone: the requested model when the
	// window has it, else the current one. Liveness, outages, internals and the
	// router's split stay the engine's, whichever model is selected.
	Models        []EngineModelRow `json:"models"`
	SelectedModel string           `json:"selectedModel,omitempty"`

	Days  []EngineDay  `json:"days"`
	Total EngineTotals `json:"total"`

	// RouterAvailable distinguishes "the router said nothing ran locally" from
	// "we could not ask the router". In a postmortem those look identical.
	RouterAvailable bool               `json:"routerAvailable"`
	RouterWindow    string             `json:"routerWindow,omitempty"`
	LocalRequests   int64              `json:"localRequests"`
	RemoteRequests  int64              `json:"remoteRequests"`
	UnknownRequests int64              `json:"unknownRequests"`
	Routing         []EngineRoutingRow `json:"routing"`
	// RouterStatusAvailable is whether the circuit fields on Routing were
	// filled from the router's live status.
	RouterStatusAvailable bool `json:"routerStatusAvailable"`

	// Today's share from the day meter (RouterDay is its local date).
	// RouterDayMetered is false when no reading has landed in today yet.
	RouterDay            string             `json:"routerDay,omitempty"`
	RouterDayMetered     bool               `json:"routerDayMetered"`
	TodayLocalRequests   int64              `json:"todayLocalRequests"`
	TodayRemoteRequests  int64              `json:"todayRemoteRequests"`
	TodayUnknownRequests int64              `json:"todayUnknownRequests"`
	RoutingToday         []EngineRoutingRow `json:"routingToday"`
}

// EngineGlance is the miniapp.engine.glance response: the engine's standing
// from state the gateway already holds, with no probe — cheap enough for a
// list tile.
//
//deneb:wire
type EngineGlance struct {
	NowMs           int64 `json:"nowMs"`
	Configured      bool  `json:"configured"`
	LivenessTracked bool  `json:"livenessTracked"`
	EngineDown      bool  `json:"engineDown"`
	// SinceMs is when the current standing began: down since, or up since.
	SinceMs    int64  `json:"sinceMs,omitempty"`
	DownReason string `json:"downReason,omitempty"`
	Model      string `json:"model,omitempty"`
	// Today, from the stores: the engine's decode rate, its outages, and the
	// router's local/remote split.
	DecodeTokensPerSec  float64 `json:"decodeTokensPerSec,omitempty"`
	TodayOutages        int     `json:"todayOutages"`
	TodayDownSeconds    float64 `json:"todayDownSeconds,omitempty"`
	TodayLocalRequests  int64   `json:"todayLocalRequests"`
	TodayRemoteRequests int64   `json:"todayRemoteRequests"`
}

// LivenessSource is the liveness watcher as the RPC sees it
// (*enginelive.Watcher implements it; its methods are nil-safe).
type LivenessSource interface {
	State(endpoint string) (enginelive.EngineState, bool)
	Transitions(endpoint string, sinceMs int64) []enginelive.Transition
	TrackedSinceMs(endpoint string) (int64, bool)
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
	// RouterEntries lists every entry name in the router's current config, so
	// a metered row can be told apart from one the config no longer knows.
	RouterEntries func() map[string]bool
	// Liveness resolves the engine liveness watcher; nil (or a nil watcher)
	// means the gateway is not probing.
	Liveness func() LivenessSource
	// RouterShare resolves the per-day router meter history.
	RouterShare func() *routershare.Store
	// Now is the clock; nil means time.Now. Tests freeze it.
	Now func() time.Time
}

func (d EngineDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d EngineDeps) endpoint() string {
	if d.Endpoints == nil {
		return ""
	}
	if eps := d.Endpoints(); len(eps) > 0 {
		return eps[0]
	}
	return ""
}

func (d EngineDeps) liveness() LivenessSource {
	if d.Liveness == nil {
		return nil
	}
	return d.Liveness()
}

// EngineMethods returns the Mini App engine RPC handlers.
func EngineMethods(deps EngineDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.engine.status": engineStatus(deps),
		"miniapp.engine.glance": engineGlance(deps),
	}
}

// engineHistoryDays is how much of the day history the panel shows. The stores
// retain 30; a phone screen reads a week.
const engineHistoryDays = 7

// engineStatusParams: Model picks whose statistics come back; empty (or a
// model the window does not have) means the engine's current model.
type engineStatusParams struct {
	Model string `json:"model"`
}

func engineStatus(deps EngineDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.engine.status requires client identity context").Response(req.ID)
		}
		var params engineStatusParams
		if len(req.Params) > 0 { // params are optional: no model means the current one
			p, errResp := rpcutil.DecodeParams[engineStatusParams](req)
			if errResp != nil {
				return errResp
			}
			params = p
		}
		now := deps.now()
		out := EngineStatusResult{
			NowMs: now.UnixMilli(), Days: []EngineDay{}, Routing: []EngineRoutingRow{},
			RoutingToday: []EngineRoutingRow{}, Outages: []EngineOutage{}, DownModels: []string{},
		}

		endpoint := deps.endpoint()
		currentRuntime := ""
		if endpoint != "" {
			out.Configured = true
			out.Endpoint = endpoint
			if c, ok := observe.FetchEngineCounters(ctx, endpoint); ok {
				currentRuntime = c.Model + " " + c.Diagnostics.Identity
				out.Reachable = true
				out.Model = c.Model
				out.RunningRequests = int(c.RunningRequests)
				out.WaitingRequests = int(c.WaitingRequests)
				out.Internals = engineInternalsFrom(c.Internals)
				if fleet, ok := observe.FetchEngineFleet(ctx, endpoint); ok {
					applyFleet(&out.Internals, fleet)
				}
			}
		}

		var downtime map[string]enginelive.DayDowntime
		var trackedSince int64
		if live := deps.liveness(); live != nil && endpoint != "" {
			if st, ok := live.State(endpoint); ok {
				out.LivenessTracked = true
				out.EngineDown = st.Down
				out.DownReason = st.Reason
				if st.Down {
					out.DownSinceMs = st.DownSince.UnixMilli()
					out.DownModels = append(out.DownModels, st.Models...)
				} else if !st.UpSince.IsZero() {
					out.UpSinceMs = st.UpSince.UnixMilli()
				}
				if since, ok := live.TrackedSinceMs(endpoint); ok {
					out.TrackedSinceMs, trackedSince = since, since
				}
				windowStart := now.AddDate(0, 0, -engineHistoryDays)
				outages := enginelive.Outages(live.Transitions(endpoint, windowStart.UnixMilli()))
				out.Outages = engineOutagesFrom(outages, now)
				downtime = enginelive.DowntimeByDay(outages, now.UnixMilli(), time.Local)
			}
		}

		var speedRows []enginespeed.DayStat
		var diagnostics []enginespeed.DiagnosticInterval
		current := out.Model
		haveSpeed := false
		if deps.Speed != nil {
			if store := deps.Speed(); store != nil {
				haveSpeed = true
				speedRows = store.Days(engineHistoryDays)
				diagnostics = store.Diagnostics(endpoint)
				if current == "" {
					current = store.CurrentModel(endpoint)
				}
			}
		}
		out.Models = engineModelRows(speedRows, current)
		out.SelectedModel = pickEngineModel(params.Model, out.Models)
		if haveSpeed {
			runtime := currentRuntime // the live runtime is only this view's when it serves the selected model
			if out.SelectedModel != out.Model {
				runtime = ""
			}
			out.Diagnostics = engineDiagnostics(diagnosticsOfModel(diagnostics, out.SelectedModel), now, runtime)
		}
		modelRows, otherModelDays := rowsOfModel(speedRows, out.SelectedModel)
		var local, known map[string]bool
		var baseURL, token string
		if deps.RouterMeter != nil {
			baseURL, token, local = deps.RouterMeter()
		}
		if deps.RouterEntries != nil {
			known = deps.RouterEntries()
		}
		var shareRows []routershare.DayEntry
		if deps.RouterShare != nil {
			if store := deps.RouterShare(); store != nil {
				shareRows = store.Days(engineHistoryDays)
			}
		}
		// The engine's downtime and the router's split belong to the engine, not
		// to a model: they ride the current model's day rows as before, and a
		// past model's view lists only the days it was measured on — without
		// the liveness flag too, or a day it lost hours on would read "no outage".
		dayDowntime, dayShare, dayTracked := downtime, shareRows, trackedSince
		if current != "" && out.SelectedModel != current {
			dayDowntime, dayShare, dayTracked, otherModelDays = nil, nil, 0, nil
		}
		out.Days, out.Total = assembleEngineDays(now, modelRows, dayDowntime, dayTracked, dayShare, local, known, otherModelDays)
		fillRouterToday(&out, now, shareRows, local, known)

		if baseURL != "" {
			if usage, ok := observe.FetchRouterUsage(ctx, baseURL, token); ok {
				out.RouterAvailable = true
				out.RouterWindow = usage.Window
				out.Routing, out.LocalRequests, out.RemoteRequests, out.UnknownRequests = routingRowsFrom(usage.Models, local, known)
			}
			if status, ok := observe.FetchRouterStatus(ctx, baseURL, token); ok {
				out.RouterStatusAvailable = true
				applyRouterStatus(out.Routing, status)
				applyRouterStatus(out.RoutingToday, status)
			}
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

func engineGlance(deps EngineDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.engine.glance requires client identity context").Response(req.ID)
		}
		now := deps.now()
		out := EngineGlance{NowMs: now.UnixMilli()}
		endpoint := deps.endpoint()
		if endpoint == "" {
			return rpcutil.RespondOK(req.ID, out)
		}
		out.Configured = true
		today := now.Format("2006-01-02")
		if live := deps.liveness(); live != nil {
			if st, ok := live.State(endpoint); ok {
				out.LivenessTracked = true
				out.EngineDown = st.Down
				out.DownReason = st.Reason
				if st.Down {
					out.SinceMs = st.DownSince.UnixMilli()
				} else if !st.UpSince.IsZero() {
					out.SinceMs = st.UpSince.UnixMilli()
				}
				dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
				outages := enginelive.Outages(live.Transitions(endpoint, dayStart.UnixMilli()))
				if d, ok := enginelive.DowntimeByDay(outages, now.UnixMilli(), time.Local)[today]; ok {
					out.TodayOutages, out.TodayDownSeconds = d.Episodes, d.Seconds
				}
			}
		}
		if deps.Speed != nil {
			if store := deps.Speed(); store != nil {
				// Today can hold one row per model; the tile speaks for the model
				// the engine served at its last scrape, not whichever sorts first.
				out.Model = store.CurrentModel(endpoint)
				for _, d := range store.Days(1) {
					if d.Day != today {
						break
					}
					if out.Model != "" && d.Model != out.Model {
						continue
					}
					if r := d.Rates(); r.Measured() {
						out.DecodeTokensPerSec = r.DecodeTokensPerSec
					}
					break
				}
			}
		}
		if deps.RouterShare != nil {
			var local map[string]bool
			if deps.RouterMeter != nil {
				_, _, local = deps.RouterMeter()
			}
			if store := deps.RouterShare(); store != nil {
				for _, e := range store.Days(1) {
					if e.Day != today {
						break
					}
					if local[e.Model] {
						out.TodayLocalRequests += e.Requests
					} else {
						out.TodayRemoteRequests += e.Requests
					}
				}
			}
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}
