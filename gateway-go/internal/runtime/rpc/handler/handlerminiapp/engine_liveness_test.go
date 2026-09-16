package handlerminiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/routershare"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const testEndpoint = "http://10.0.0.5:8000/metrics"

// fakeLiveness is a LivenessSource with a scripted state and ledger.
type fakeLiveness struct {
	state   enginelive.EngineState
	ledger  *enginelive.Ledger
	tracked int64
}

func (f *fakeLiveness) State(endpoint string) (enginelive.EngineState, bool) {
	if endpoint != testEndpoint {
		return enginelive.EngineState{}, false
	}
	return f.state, true
}

func (f *fakeLiveness) Transitions(endpoint string, sinceMs int64) []enginelive.Transition {
	return f.ledger.Transitions(endpoint, sinceMs)
}

func (f *fakeLiveness) TrackedSinceMs(string) (int64, bool) { return f.tracked, f.tracked > 0 }

func glanceResult(ctx context.Context, t *testing.T, deps EngineDeps) EngineGlance {
	t.Helper()
	resp := EngineMethods(deps)["miniapp.engine.glance"](ctx, &protocol.RequestFrame{ID: "1"})
	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var out EngineGlance
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// The day the engine was gone for entirely has no scrape and no counters —
// and it is exactly the day that must not be missing. The ledger puts it in
// the list, with its downtime, and the status line carries the gateway's
// routing verdict rather than a probe made for the screen.
func TestEngineStatusCarriesLivenessAndDowntimeDays(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 9, 16, 22, 0, 0, 0, loc)
	ledger := enginelive.NewLedger("")
	at := func(d time.Time) int64 { return d.UnixMilli() }
	// The 14th: down the whole day (began the 13th 22:00, up the 15th 02:00).
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: at(time.Date(2026, 9, 13, 22, 0, 0, 0, loc)), Down: true, Reason: "connection refused"})
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: at(time.Date(2026, 9, 15, 2, 0, 0, 0, loc)), Down: false})
	// Today: two short outages, the second still open.
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: at(time.Date(2026, 9, 16, 10, 0, 0, 0, loc)), Down: true, Reason: "connection refused"})
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: at(time.Date(2026, 9, 16, 10, 30, 0, 0, loc)), Down: false})
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: at(time.Date(2026, 9, 16, 21, 0, 0, 0, loc)), Down: true, Reason: "health 503 draining"})

	live := &fakeLiveness{
		state: enginelive.EngineState{
			Endpoint: testEndpoint, Down: true, Probed: true,
			DownSince: time.Date(2026, 9, 16, 21, 0, 0, 0, loc), Reason: "health 503 draining",
			Models: []string{"glm-5.3-flash", "glm-5.3-flash-low"},
		},
		ledger:  ledger,
		tracked: at(time.Date(2026, 9, 13, 0, 0, 0, 0, loc)),
	}
	speed := enginespeed.NewStore("")
	// The engine measured the 15th and 16th; nothing on the 14th.
	base := observe.EngineCounters{TTFTCount: 10, TTFTSeconds: 20, TPOTSeconds: 10, TPOTCount: 500, GenerationTokens: 510, PromptTokens: 1000, BusySeconds: 30}
	for _, day := range []time.Time{time.Date(2026, 9, 15, 12, 0, 0, 0, loc), time.Date(2026, 9, 16, 12, 0, 0, 0, loc)} {
		speed.Observe(testEndpoint, day, 15*time.Second, base)
		later := base
		later.TTFTCount += 10
		later.TTFTSeconds += 20
		later.TPOTSeconds += 10
		later.TPOTCount += 500
		later.GenerationTokens += 510
		later.PromptTokens += 1000
		later.BusySeconds += 30
		speed.Observe(testEndpoint, day.Add(15*time.Second), 15*time.Second, later)
	}

	out := engineResult(clientauth.WithContext(context.Background(), sampleIdentity()), t, EngineDeps{
		Endpoints: func() []string { return []string{testEndpoint} },
		Liveness:  func() LivenessSource { return live },
		Speed:     func() *enginespeed.Store { return speed },
		Now:       func() time.Time { return now },
	})

	if !out.LivenessTracked || !out.EngineDown || out.DownReason != "health 503 draining" {
		t.Fatalf("liveness verdict not carried: %+v", out)
	}
	if out.DownSinceMs != at(time.Date(2026, 9, 16, 21, 0, 0, 0, loc)) || len(out.DownModels) != 2 {
		t.Errorf("down since/models = %d %v", out.DownSinceMs, out.DownModels)
	}
	if out.NowMs != now.UnixMilli() || out.TrackedSinceMs != live.tracked {
		t.Errorf("clock/tracked = %d/%d", out.NowMs, out.TrackedSinceMs)
	}
	if len(out.Outages) != 3 || out.Outages[0].UntilMs != 0 || out.Outages[0].DurationSec != 3600 || out.Outages[2].Reason != "connection refused" {
		t.Fatalf("outages newest first, the open one closed at now: %+v", out.Outages)
	}

	days := map[string]EngineDay{}
	for _, d := range out.Days {
		days[d.Day] = d
	}
	d14, ok := days["2026-09-14"]
	if !ok {
		t.Fatalf("the fully-down day must be in the list: %+v", out.Days)
	}
	if d14.Measured || !d14.LivenessTracked || d14.DownSeconds != 86400 || d14.Outages != 0 {
		t.Errorf("14th = %+v, want unmeasured, tracked, 86400s down, no episode of its own", d14)
	}
	d16 := days["2026-09-16"]
	if !d16.Measured || d16.Outages != 2 || d16.DownSeconds != 1800+3600 {
		t.Errorf("16th = %+v, want measured, 2 episodes, 5400s", d16)
	}
	if d13 := days["2026-09-13"]; d13.Outages != 1 || d13.DownSeconds != 7200 {
		t.Errorf("13th = %+v, want the episode that began there and its two hours", d13)
	}
	if out.Days[0].Day != "2026-09-16" || out.Days[len(out.Days)-1].Day != "2026-09-13" {
		t.Errorf("days must be newest first: %+v", out.Days)
	}
	if out.Total.Outages != 3 || out.Total.DownSeconds != 7200+86400+7200+5400 {
		t.Errorf("totals = outages %d down %v", out.Total.Outages, out.Total.DownSeconds)
	}
	if out.Total.Requests != 20 || out.Total.Days != 4 {
		t.Errorf("engine totals must still sum the measured days: %+v", out.Total)
	}
}

// The month meter straddles routing changes; the day meter does not. Today's
// split comes from it, and an entry the router config no longer lists is
// unknown — never silently remote.
func TestEngineStatusReadsTodayFromTheDayMeterAndFlagsUnknownEntries(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 9, 16, 22, 0, 0, 0, loc)
	share := routershare.NewStore("")
	usage := func(reqLocal, reqCloud, reqGone int64) observe.RouterUsage {
		return observe.RouterUsage{Window: "2026-09", Models: []observe.RouterModelUsage{
			{Model: "glm-5.3-flash", Requests: reqLocal},
			{Model: "glm-5.3", Requests: reqCloud},
			{Model: "glm-5.3-flash-local", Requests: reqGone},
		}}
	}
	share.Observe(now.Add(-2*time.Hour), usage(100, 50, 30))
	share.Observe(now.Add(-time.Hour), usage(160, 70, 30))
	share.Observe(now.Add(-25*time.Hour), usage(0, 0, 0)) // yesterday: baseline only, no rows

	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/usage":
			_ = json.NewEncoder(w).Encode(usage(5324, 2073, 838))
		case "/status":
			_, _ = w.Write([]byte(`{"models":[{"name":"glm-5.3-flash","local":true,"circuitState":"open","retryAfterMs":540000},
				{"name":"glm-5.3","local":false,"circuitState":"closed","keyHealth":"unreachable"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer router.Close()

	out := engineResult(clientauth.WithContext(context.Background(), sampleIdentity()), t, EngineDeps{
		Endpoints: func() []string { return []string{testEndpoint} },
		RouterMeter: func() (string, string, map[string]bool) {
			return router.URL, "", map[string]bool{"glm-5.3-flash": true}
		},
		RouterEntries: func() map[string]bool { return map[string]bool{"glm-5.3-flash": true, "glm-5.3": true} },
		RouterShare:   func() *routershare.Store { return share },
		Now:           func() time.Time { return now },
	})

	if !out.RouterDayMetered || out.RouterDay != "2026-09-16" {
		t.Fatalf("today must come from the day meter: %+v", out)
	}
	if out.TodayLocalRequests != 60 || out.TodayRemoteRequests != 20 || out.TodayUnknownRequests != 0 {
		t.Errorf("today = local %d remote %d unknown %d, want 60/20/0", out.TodayLocalRequests, out.TodayRemoteRequests, out.TodayUnknownRequests)
	}
	if len(out.RoutingToday) != 2 || out.RoutingToday[0].Model != "glm-5.3-flash" || out.RoutingToday[0].CircuitState != "open" {
		t.Errorf("today's rows must be sorted and stamped with the circuit: %+v", out.RoutingToday)
	}
	if !out.RouterStatusAvailable {
		t.Error("router status answered")
	}
	// Month rows: the renamed entry is unknown, not remote.
	if out.LocalRequests != 5324 || out.RemoteRequests != 2073 || out.UnknownRequests != 838 {
		t.Errorf("month = local %d remote %d unknown %d", out.LocalRequests, out.RemoteRequests, out.UnknownRequests)
	}
	for _, row := range out.Routing {
		switch row.Model {
		case "glm-5.3-flash-local":
			if row.Known || row.Local {
				t.Errorf("a renamed entry must be unknown: %+v", row)
			}
		case "glm-5.3":
			if !row.Known || row.Local || row.KeyHealth != "unreachable" {
				t.Errorf("cloud row = %+v", row)
			}
		case "glm-5.3-flash":
			if !row.Known || !row.Local || row.CircuitState != "open" || row.RetryAfterMs != 540000 {
				t.Errorf("local row = %+v", row)
			}
		}
	}
	// The day rows carry the split too.
	if len(out.Days) != 1 || out.Days[0].Day != "2026-09-16" || !out.Days[0].RouterMetered ||
		out.Days[0].RouterLocalRequests != 60 || out.Days[0].RouterRemoteRequests != 20 {
		t.Errorf("day row split = %+v", out.Days)
	}
	if out.Total.RouterLocalRequests != 60 || out.Total.RouterRemoteRequests != 20 {
		t.Errorf("totals split = %+v", out.Total)
	}
}

// The glance answers from held state only — no probe, no router call.
func TestEngineGlanceNeedsNoNetwork(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 9, 16, 22, 0, 0, 0, loc)
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	none := glanceResult(ctx, t, EngineDeps{Endpoints: func() []string { return nil }, Now: func() time.Time { return now }})
	if none.Configured || none.LivenessTracked {
		t.Errorf("no endpoint → unconfigured: %+v", none)
	}

	ledger := enginelive.NewLedger("")
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: time.Date(2026, 9, 16, 10, 0, 0, 0, loc).UnixMilli(), Down: true, Reason: "connection refused"})
	ledger.Record(enginelive.Transition{Endpoint: testEndpoint, AtMs: time.Date(2026, 9, 16, 10, 30, 0, 0, loc).UnixMilli(), Down: false})
	live := &fakeLiveness{state: enginelive.EngineState{
		Endpoint: testEndpoint, Probed: true,
		UpSince: time.Date(2026, 9, 16, 10, 30, 0, 0, loc),
	}, ledger: ledger, tracked: 1}
	share := routershare.NewStore("")
	share.Observe(now.Add(-2*time.Hour), observe.RouterUsage{Window: "2026-09", Models: []observe.RouterModelUsage{{Model: "glm-5.3-flash", Requests: 1}, {Model: "k3", Requests: 1}}})
	share.Observe(now.Add(-time.Hour), observe.RouterUsage{Window: "2026-09", Models: []observe.RouterModelUsage{{Model: "glm-5.3-flash", Requests: 8}, {Model: "k3", Requests: 3}}})

	out := glanceResult(ctx, t, EngineDeps{
		Endpoints:   func() []string { return []string{"http://127.0.0.1:1/metrics"} }, // nothing listens; must not matter
		Liveness:    func() LivenessSource { return live },
		RouterMeter: func() (string, string, map[string]bool) { return "", "", map[string]bool{"glm-5.3-flash": true} },
		RouterShare: func() *routershare.Store { return share },
		Now:         func() time.Time { return now },
	})
	_ = testEndpoint
	if !out.Configured || out.NowMs != now.UnixMilli() {
		t.Fatalf("glance = %+v", out)
	}
	// The fake keys on testEndpoint; a different endpoint reads as untracked.
	if out.LivenessTracked {
		t.Errorf("an endpoint the watcher does not track must read as untracked: %+v", out)
	}
	if out.TodayLocalRequests != 7 || out.TodayRemoteRequests != 2 {
		t.Errorf("today split = %d/%d, want 7/2", out.TodayLocalRequests, out.TodayRemoteRequests)
	}

	tracked := glanceResult(ctx, t, EngineDeps{
		Endpoints: func() []string { return []string{testEndpoint} },
		Liveness:  func() LivenessSource { return live },
		Now:       func() time.Time { return now },
	})
	if !tracked.LivenessTracked || tracked.EngineDown || tracked.SinceMs != live.state.UpSince.UnixMilli() {
		t.Errorf("tracked glance = %+v", tracked)
	}
	if tracked.TodayOutages != 1 || tracked.TodayDownSeconds != 1800 {
		t.Errorf("today's downtime = %d/%v, want 1/1800", tracked.TodayOutages, tracked.TodayDownSeconds)
	}
}

func TestEngineGlanceRequiresIdentity(t *testing.T) {
	resp := EngineMethods(EngineDeps{})["miniapp.engine.glance"](context.Background(), &protocol.RequestFrame{ID: "1"})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("an unauthenticated call must be refused: OK=%v err=%+v", resp.OK, resp.Error)
	}
}
