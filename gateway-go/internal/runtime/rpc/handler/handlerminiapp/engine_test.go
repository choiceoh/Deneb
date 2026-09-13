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
