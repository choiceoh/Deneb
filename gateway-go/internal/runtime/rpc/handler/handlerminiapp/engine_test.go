package handlerminiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func engineResult(ctx context.Context, t *testing.T, deps EngineDeps) EngineStatusResult {
	t.Helper()
	resp := EngineMethods(deps)["miniapp.engine.status"](ctx, &protocol.RequestFrame{ID: "1"})
	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var out EngineStatusResult
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestEngineStatusRequiresIdentity(t *testing.T) {
	resp := EngineMethods(EngineDeps{})["miniapp.engine.status"](context.Background(), &protocol.RequestFrame{ID: "1"})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("an unauthenticated call must be refused: OK=%v err=%+v", resp.OK, resp.Error)
	}
}

// "No engine configured" and "configured but unreachable" are different
// operational states and the app renders them differently.
func TestEngineStatusSeparatesUnconfiguredFromUnreachable(t *testing.T) {
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	none := engineResult(ctx, t, EngineDeps{Endpoints: func() []string { return nil }})
	if none.Configured || none.Reachable {
		t.Errorf("no endpoint must read as unconfigured: %+v", none)
	}

	down := engineResult(ctx, t, EngineDeps{
		// A private address nothing listens on: configured, not reachable.
		Endpoints: func() []string { return []string{"http://127.0.0.1:1/metrics"} },
	})
	if !down.Configured {
		t.Error("a configured endpoint must read as configured even when down")
	}
	if down.Reachable {
		t.Error("nothing is listening; Reachable must be false")
	}
}

func TestEngineStatusReportsLiveCountersAndDays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("vllm:num_requests_running{engine=\"st\"} 2\n" +
				"vllm:num_requests_waiting{engine=\"st\"} 1\n" +
				"vllm:prompt_tokens_total{engine=\"st\"} 100\n"))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"glm-5.3-flash"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	store := enginespeed.NewStore(t.TempDir() + "/engine-speed.json")
	out := engineResult(clientauth.WithContext(context.Background(), sampleIdentity()), t, EngineDeps{
		Endpoints: func() []string { return []string{srv.URL + "/metrics"} },
		Speed:     func() *enginespeed.Store { return store },
	})

	if !out.Reachable {
		t.Fatal("a live engine must read as reachable")
	}
	if out.Model != "glm-5.3-flash" {
		t.Errorf("Model = %q", out.Model)
	}
	if out.RunningRequests != 2 || out.WaitingRequests != 1 {
		t.Errorf("in-flight = %d/%d, want 2/1", out.RunningRequests, out.WaitingRequests)
	}
	if out.Days == nil || out.Routing == nil {
		t.Error("empty lists must marshal as [] so the app never sees null")
	}
}

// The share is the reason this RPC exists: speed alone reads as if every turn
// ran locally. An unreachable meter must be distinguishable from a zero share.
func TestEngineStatusCarriesTheRouterShare(t *testing.T) {
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(observe.RouterUsage{
			Window: "2026-09",
			Models: []observe.RouterModelUsage{
				{Model: "glm-5.3-flash", Requests: 4412},
				{Model: "glm-5.3-flash-local", Requests: 838},
			},
		})
	}))
	defer router.Close()
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	out := engineResult(ctx, t, EngineDeps{
		RouterMeter: func() (string, string, map[string]bool) {
			return router.URL, "", map[string]bool{"glm-5.3-flash-local": true}
		},
	})
	if !out.RouterAvailable || out.RouterWindow != "2026-09" {
		t.Fatalf("meter not reported: %+v", out)
	}
	if out.LocalRequests != 838 || out.RemoteRequests != 4412 {
		t.Errorf("share = %d/%d, want 838/4412", out.LocalRequests, out.RemoteRequests)
	}
	if len(out.Routing) != 2 || out.Routing[0].Model != "glm-5.3-flash" {
		t.Errorf("routing rows not sorted by requests: %+v", out.Routing)
	}
	if !out.Routing[1].Local {
		t.Error("the local entry must be marked local")
	}

	blind := engineResult(ctx, t, EngineDeps{
		RouterMeter: func() (string, string, map[string]bool) { return "http://127.0.0.1:1", "", nil },
	})
	if blind.RouterAvailable {
		t.Error("an unreachable meter must not claim availability")
	}
	if blind.LocalRequests != 0 || blind.RemoteRequests != 0 {
		t.Error("an unreachable meter must report no counts at all")
	}
}

// Window rates are recomputed from the SUMMED deltas, never averaged from the
// daily rates. Averaging would let a day with four requests weigh as much as a
// day with four hundred, which is how a quiet Sunday drags a week's decode
// figure down by a third.
func TestEngineTotalsWeighByWorkNotByDay(t *testing.T) {
	// A busy day and a quiet one, decoding at very different rates.
	busy := observe.EngineDelta{TPOTCount: 9000, TPOTSeconds: 300, Requests: 90, TTFTSeconds: 90}
	quiet := observe.EngineDelta{TPOTCount: 10, TPOTSeconds: 10, Requests: 10, TTFTSeconds: 100}

	got := engineTotalsFrom(EngineTotals{}, 2, 1000, busy.Add(quiet))

	// Weighted: 9010 tokens over 310 seconds.
	want := 9010.0 / 310.0
	if diff := got.DecodeTokensPerSec - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("DecodeTokensPerSec = %.3f, want the work-weighted %.3f", got.DecodeTokensPerSec, want)
	}
	// The naive average of the two daily rates (30 and 1) is 15.5 — far off.
	if got.DecodeTokensPerSec < 20 {
		t.Errorf("rate %.3f looks averaged per day, not weighted by work", got.DecodeTokensPerSec)
	}
	if got.Requests != 100 {
		t.Errorf("Requests = %d, want 100", got.Requests)
	}
	// Mean first-token latency is likewise mass-weighted: 190s over 100 requests.
	if diff := got.MeanTtftSeconds - 1.9; diff > 0.01 || diff < -0.01 {
		t.Errorf("MeanTtftSeconds = %.3f, want 1.9", got.MeanTtftSeconds)
	}
}

// Utilization divides stepping time by the time the sampler actually WATCHED,
// not by the hours in a day: the gateway is not always running, and a day it
// only saw an hour of is not a day the engine was idle for twenty-three.
func TestEngineUtilizationUsesObservedTimeNotWallClock(t *testing.T) {
	day := enginespeed.DayStat{Polls: 240, PollIntervalSec: 15} // one hour watched
	if got := observedSeconds(day); got != 3600 {
		t.Fatalf("observedSeconds = %v, want 3600", got)
	}
	totals := engineTotalsFrom(EngineTotals{}, 1, observedSeconds(day), observe.EngineDelta{BusySeconds: 1800})
	if diff := totals.Utilization - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Utilization = %v, want 0.5 (half of the watched hour)", totals.Utilization)
	}
	// No polls means no window; a ratio would be an invention.
	if got := observedSeconds(enginespeed.DayStat{Polls: 0, PollIntervalSec: 15}); got != 0 {
		t.Errorf("observedSeconds with no polls = %v, want 0", got)
	}
}

// The prompt-cache ratio is token-valued on this engine, and it is the number
// with the largest lever behind it — a head the engine already holds costs
// nothing to prefill.
func TestEngineDayCarriesThePromptCacheRatio(t *testing.T) {
	day := enginespeed.DayStat{
		Day: "2026-09-13", Polls: 240, PollIntervalSec: 15,
		Delta: observe.EngineDelta{
			PrefixCacheQueries: 100, PrefixCacheHits: 10, CachePromptTokens: 10_000, CachedPromptTokens: 7_500,
			Requests: 10, TTFTSeconds: 10, TPOTCount: 100, TPOTSeconds: 2,
		},
	}
	got := engineDayFrom(day)
	if diff := got.PromptCacheHitRatio - 0.75; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("PromptCacheHitRatio = %v, want 0.75", got.PromptCacheHitRatio)
	}
	if !got.PromptCacheMeasured || got.CachePromptTokens != 10000 || got.PrefixRequestHitRatio != 0.1 || got.PrefixHitRequests != 10 || got.PrefixLookupRequests != 100 {
		t.Fatalf("token and request units crossed in RPC: %+v", got)
	}
	if got.CachedPromptTokens != 7500 {
		t.Errorf("CachedPromptTokens = %d, want 7500", got.CachedPromptTokens)
	}
}
