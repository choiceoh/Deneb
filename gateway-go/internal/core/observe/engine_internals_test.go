package observe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The engine's own gauges ride the same scrape; they are point-in-time and
// explain the cache hit ratio (no free snapshot = nothing new can be resumed).
func TestFetchEngineCountersReadsInternalsAndSpecDecode(t *testing.T) {
	body := engineMetricsBody(10, 1, 20, 2000, 100, 5000, 2000, 5, 15, 1, 0) +
		"vllm:spec_decode_num_draft_tokens_total{engine=\"st\"} 58919\n" +
		"vllm:spec_decode_num_accepted_tokens_total{engine=\"st\"} 16631\n" +
		"st:prefix_entries{engine=\"st\"} 96\n" +
		"st:prefix_faded_entries{engine=\"st\"} 4\n" + // a longer name sharing a prefix: not folded in
		"st:prefix_snapshots_free{engine=\"st\"} 0\n" +
		"st:prefix_snapshot_denials_total{engine=\"st\"} 7\n" +
		"st:prefix_pinned_entries{engine=\"st\"} 2\n" +
		"st:prefix_tier_entries{engine=\"st\"} 54\n" +
		"st:kv_blocks_total{engine=\"st\"} 2987\n" +
		"st:kv_blocks_used{engine=\"st\"} 12\n" +
		"st:kv_blocks_cached{engine=\"st\"} 96\n" +
		"st:conversations_parked{engine=\"st\"} 46\n" +
		"st:device_memory_total_bytes{engine=\"st\"} 128520081408\n" +
		"st:device_memory_free_bytes{engine=\"st\"} 17813172224\n" +
		"st:device_memory_reserved_bytes{engine=\"st\"} 72301412352\n" +
		"st:host_memory_available_bytes{engine=\"st\"} 16106307584\n" +
		"st:handing_over{engine=\"st\"} 1\n" +
		"st:quiet{engine=\"st\"} 0\n"
	srv := engineServer(t, body)
	defer srv.Close()

	c, ok := FetchEngineCounters(context.Background(), srv.URL+"/metrics")
	if !ok {
		t.Fatal("scrape failed")
	}
	if c.SpecDraftTokens != 58919 || c.SpecAcceptedTokens != 16631 {
		t.Errorf("spec = %v/%v", c.SpecAcceptedTokens, c.SpecDraftTokens)
	}
	in := c.Internals
	if !in.Published {
		t.Fatal("st: gauges present → Published")
	}
	if in.PrefixEntries != 96 || in.PrefixSnapshotsFree != 0 || in.PrefixPinnedEntries != 2 || in.PrefixTierEntries != 54 {
		t.Errorf("prefix = %+v", in)
	}
	if in.KVBlocksTotal != 2987 || in.KVBlocksUsed != 12 || in.KVBlocksCached != 96 || in.ConversationsParked != 46 {
		t.Errorf("kv = %+v", in)
	}
	if in.DeviceMemoryTotalBytes != 128520081408 || in.DeviceMemoryFreeBytes != 17813172224 ||
		in.DeviceMemoryReservedBytes != 72301412352 || in.HostMemoryAvailableBytes != 16106307584 {
		t.Errorf("memory = %+v", in)
	}
	if !in.HandingOver || in.Quiet {
		t.Errorf("flags = handingOver %v quiet %v", in.HandingOver, in.Quiet)
	}

	// The window's acceptance ratio comes from the deltas, like every rate.
	later := c
	later.SpecDraftTokens += 1000
	later.SpecAcceptedTokens += 280
	d, ok := EngineDeltaBetween(c, later)
	if !ok {
		t.Fatal("forward counters must difference")
	}
	r := d.Rates()
	if r.SpecAcceptRatio != 0.28 || r.SpecDraftTokens != 1000 {
		t.Errorf("spec accept = %v over %d, want 0.28 over 1000", r.SpecAcceptRatio, r.SpecDraftTokens)
	}
	// A drafter counter moving backwards is a restart like any other.
	if _, ok := EngineDeltaBetween(later, c); ok {
		t.Error("backwards spec counters must be refused")
	}
}

// An engine that exports only the vLLM series has no internals block — that
// is "not published", never "zero memory".
func TestEngineInternalsUnpublishedWithoutStGauges(t *testing.T) {
	srv := engineServer(t, engineMetricsBody(10, 1, 20, 2000, 100, 5000, 2000, 5, 15, 0, 0))
	defer srv.Close()
	c, ok := FetchEngineCounters(context.Background(), srv.URL+"/metrics")
	if !ok {
		t.Fatal("scrape failed")
	}
	if c.Internals.Published {
		t.Errorf("no st: gauges → unpublished: %+v", c.Internals)
	}
}

func TestFetchEngineFleetReadsTheRootStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"engine": "ST", "model": "glm-5.3-flash", "running": []string{}, "waiting": []string{},
			"queued": 0, "parked": 46, "steps": 7107, "served": 166,
			"fleet": map[string]any{
				"owner": "production/deploy/2011680", "path": "/x/st-fleet.lock",
				"draining": nil, "handed_over": nil,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f, ok := FetchEngineFleet(context.Background(), srv.URL+"/metrics")
	if !ok {
		t.Fatal("fleet read failed")
	}
	if f.Served != 166 || f.Steps != 7107 || f.Parked != 46 {
		t.Errorf("counts = %+v", f)
	}
	if !f.FleetKnown || f.Owner != "production/deploy/2011680" || f.Draining != "" || f.HandedOver != "" {
		t.Errorf("fleet = %+v", f)
	}
}

func TestFetchEngineFleetRefusesNonEngineAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()
	if _, ok := FetchEngineFleet(context.Background(), srv.URL+"/metrics"); ok {
		t.Error("a document without an engine field is not a status document")
	}
	if _, ok := FetchEngineFleet(context.Background(), "http://example.com/metrics"); ok {
		t.Error("a public host must never be contacted")
	}
}

func TestFetchRouterStatusReadsCircuitsAndRefusesPublicHosts(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"listen":":18800","models":[
			{"name":"glm-5.3-flash","local":true,"circuitState":"open","circuitFailures":3,"retryAfterMs":540000},
			{"name":"glm-5.3","local":false,"circuitState":"closed","keyHealth":"unreachable","upstreamMissing":true}]}`))
	}))
	defer srv.Close()

	st, ok := FetchRouterStatus(context.Background(), srv.URL+"/v1", "tok")
	if !ok {
		t.Fatal("status read failed")
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	by := st.ByName()
	if m := by["glm-5.3-flash"]; !m.Local || m.CircuitState != "open" || m.CircuitFailures != 3 || m.RetryAfterMS != 540000 {
		t.Errorf("local row = %+v", m)
	}
	if m := by["glm-5.3"]; m.KeyHealth != "unreachable" || !m.UpstreamMissing || m.CircuitState != "closed" {
		t.Errorf("cloud row = %+v", m)
	}
	if routerStatusURL("http://api.example.com/v1") != "" {
		t.Error("a public host must never be contacted")
	}
	if got := routerStatusURL("http://127.0.0.1:18800/v1"); got != "http://127.0.0.1:18800/status" {
		t.Errorf("status url = %q", got)
	}
	if _, ok := FetchRouterStatus(context.Background(), "http://127.0.0.1:1", ""); ok {
		t.Error("an unreachable router must read as unavailable")
	}
}
