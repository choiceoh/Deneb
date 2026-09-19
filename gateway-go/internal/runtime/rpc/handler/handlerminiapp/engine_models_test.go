package handlerminiapp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// engineServing is a live engine door that names model in /v1/models.
func engineServing(t *testing.T, model string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("vllm:prompt_tokens_total{engine=\"st\"} 100\n"))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + model + `"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/metrics"
}

// ledgerLiveness is a LivenessSource for whichever endpoint the test serves.
type ledgerLiveness struct {
	endpoint string
	ledger   *enginelive.Ledger
	tracked  int64
}

func (f *ledgerLiveness) State(endpoint string) (enginelive.EngineState, bool) {
	return enginelive.EngineState{Endpoint: endpoint, Probed: true}, endpoint == f.endpoint
}

func (f *ledgerLiveness) Transitions(endpoint string, sinceMs int64) []enginelive.Transition {
	return f.ledger.Transitions(endpoint, sinceMs)
}

func (f *ledgerLiveness) TrackedSinceMs(string) (int64, bool) { return f.tracked, f.tracked > 0 }

// served folds one interval of work by model into the store at t: a baseline
// scrape and one 15 s later carrying tpotCount decoded tokens over 10 s.
func served(store *enginespeed.Store, endpoint, model string, t time.Time, tpotCount float64) {
	base := observe.EngineCounters{Model: model, TTFTCount: 10, TTFTSeconds: 20, TPOTSeconds: 10, TPOTCount: 500, GenerationTokens: 510, PromptTokens: 1000, BusySeconds: 30}
	store.Observe(endpoint, t, 15*time.Second, base)
	later := base
	later.TTFTCount += 10
	later.TTFTSeconds += 20
	later.TPOTSeconds += 10
	later.TPOTCount += tpotCount
	later.GenerationTokens += 510
	later.PromptTokens += 1000
	later.BusySeconds += 30
	store.Observe(endpoint, t.Add(15*time.Second), 15*time.Second, later)
}

func engineStatusFor(ctx context.Context, t *testing.T, deps EngineDeps, model string) EngineStatusResult {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"model": model})
	resp := EngineMethods(deps)["miniapp.engine.status"](ctx, &protocol.RequestFrame{ID: "1", Params: raw})
	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var out EngineStatusResult
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func dayList(days []EngineDay) string {
	parts := make([]string, 0, len(days))
	for _, d := range days {
		parts = append(parts, d.Day+"/"+d.Model)
	}
	return strings.Join(parts, " ")
}

// GLM-5.3 serves; Qwen3.8 ran all of the 18th and a window on the 19th. Each
// model's view is its own days; the engine's downtime rides only the current
// model's rows, and a day only Qwen was measured on is not an "unmeasured GLM
// day" — even though the engine lost two hours on it.
func TestEngineStatusShowsOneModelsStatisticsAtATime(t *testing.T) {
	loc := time.Local
	const glm, qwen = "glm-5.3-flash", "qwen3.8-flash-next"
	endpoint := engineServing(t, glm)
	now := time.Date(2026, 9, 19, 22, 0, 0, 0, loc)
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, loc) }

	store := enginespeed.NewStore("")
	served(store, endpoint, glm, day(17, 12), 500)
	served(store, endpoint, qwen, day(18, 12), 600)
	served(store, endpoint, glm, day(19, 12), 500)
	served(store, endpoint, qwen, day(19, 14), 600)
	served(store, endpoint, glm, day(19, 16), 500)

	ledger := enginelive.NewLedger("")
	ledger.Record(enginelive.Transition{Endpoint: endpoint, AtMs: day(18, 10).UnixMilli(), Down: true, Reason: "connection refused"})
	ledger.Record(enginelive.Transition{Endpoint: endpoint, AtMs: day(18, 12).UnixMilli(), Down: false})
	ledger.Record(enginelive.Transition{Endpoint: endpoint, AtMs: day(19, 8).UnixMilli(), Down: true, Reason: "connection refused"})
	ledger.Record(enginelive.Transition{Endpoint: endpoint, AtMs: day(19, 8).Add(30 * time.Minute).UnixMilli(), Down: false})

	deps := EngineDeps{
		Endpoints: func() []string { return []string{endpoint} },
		Speed:     func() *enginespeed.Store { return store },
		Liveness: func() LivenessSource {
			return &ledgerLiveness{endpoint: endpoint, ledger: ledger, tracked: day(16, 0).UnixMilli()}
		},
		Now: func() time.Time { return now },
	}
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	current := engineResult(ctx, t, deps) // no params: the current model
	if current.SelectedModel != glm || current.Model != glm {
		t.Fatalf("selected %q (live %q), want the model the engine serves", current.SelectedModel, current.Model)
	}
	if len(current.Models) != 2 || current.Models[0].Model != glm || !current.Models[0].Current ||
		current.Models[1].Model != qwen || current.Models[1].Current || current.Models[1].Days != 2 {
		t.Fatalf("models = %+v, want GLM (current) then Qwen (2 days)", current.Models)
	}
	if got := dayList(current.Days); got != "2026-09-19/"+glm+" 2026-09-17/"+glm {
		t.Fatalf("GLM view days = %s, want only GLM's (the 18th is Qwen's)", got)
	}
	if current.Days[0].DownSeconds != 1800 || !current.Days[0].LivenessTracked {
		t.Errorf("the current model's day must carry the engine's downtime: %+v", current.Days[0])
	}
	if !strings.HasPrefix(current.Diagnostics.Windows[1].Runtime, glm) {
		t.Errorf("GLM view runtime = %q", current.Diagnostics.Windows[1].Runtime)
	}

	past := engineStatusFor(ctx, t, deps, qwen)
	if past.SelectedModel != qwen {
		t.Fatalf("selected %q, want the requested model", past.SelectedModel)
	}
	if got := dayList(past.Days); got != "2026-09-19/"+qwen+" 2026-09-18/"+qwen {
		t.Fatalf("Qwen view days = %s", got)
	}
	for _, d := range past.Days {
		if d.DownSeconds != 0 || d.Outages != 0 || d.LivenessTracked || d.RouterMetered {
			t.Errorf("a past model's day claims the engine's liveness: %+v", d)
		}
	}
	if math.Abs(past.Total.DecodeTokensPerSec-60) > 0.01 {
		t.Errorf("Qwen decode = %.2f, want its own 60 tok/s, not GLM's 50", past.Total.DecodeTokensPerSec)
	}
	if !strings.HasPrefix(past.Diagnostics.Windows[1].Runtime, qwen) {
		t.Errorf("Qwen view runtime = %q, want Qwen's own", past.Diagnostics.Windows[1].Runtime)
	}
	if !past.Reachable || past.Model != glm {
		t.Errorf("the live probe is the engine's whichever model is selected: %+v", past)
	}

	if unknown := engineStatusFor(ctx, t, deps, "deepseek-v4-flash"); unknown.SelectedModel != glm {
		t.Errorf("a model the window never served selected %q, want the current one", unknown.SelectedModel)
	}
}

// Today holds a row per model; the tile speaks for the model the engine
// served last, not for whichever row sorts first.
func TestEngineGlanceReadsTheCurrentModelsRow(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 9, 19, 22, 0, 0, 0, loc)
	store := enginespeed.NewStore("")
	served(store, testEndpoint, "glm-5.3-flash", time.Date(2026, 9, 19, 12, 0, 0, 0, loc), 500)
	served(store, testEndpoint, "qwen3.8-flash-next", time.Date(2026, 9, 19, 14, 0, 0, 0, loc), 600)

	out := glanceResult(clientauth.WithContext(context.Background(), sampleIdentity()), t, EngineDeps{
		Endpoints: func() []string { return []string{testEndpoint} },
		Speed:     func() *enginespeed.Store { return store },
		Now:       func() time.Time { return now },
	})
	if out.Model != "qwen3.8-flash-next" {
		t.Fatalf("glance model = %q, want the last-served one", out.Model)
	}
	if math.Abs(out.DecodeTokensPerSec-60) > 0.01 {
		t.Errorf("glance decode = %.2f, want Qwen's row (60), not GLM's (50)", out.DecodeTokensPerSec)
	}
}
