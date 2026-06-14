package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servicesPayload is a canned SparkFleet GET /api/services response: one healthy
// vLLM (advertises a model), one down vLLM, and one non-chat sidecar (paddleocr,
// no model, /health probe URL). Only the first should become a routable model.
const servicesPayload = `{"services":[
	{"node":"gx10","name":"vllm-tp2","url":"http://127.0.0.1:8000/v1/models","ok":true,"httpStatus":200,"model":"qwen3.6-35b-a3b","nodeReachable":true},
	{"node":"gx10","name":"vllm-nex","url":"http://127.0.0.1:8002/v1/models","ok":false,"httpStatus":0,"nodeReachable":true},
	{"node":"gx10","name":"paddleocr","url":"http://127.0.0.1:18011/health","ok":true,"httpStatus":200,"nodeReachable":true}
]}`

func fleetServer(t *testing.T, wantToken string, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if wantToken != "" && r.Header.Get("X-Fleet-Token") != wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
}

func TestDiscoverFleet_HealthyVLLMOnly(t *testing.T) {
	srv := fleetServer(t, "", servicesPayload)
	defer srv.Close()

	got, err := discoverFleet(context.Background(), srv.Client(), fleetSource{URL: srv.URL})
	if err != nil {
		t.Fatalf("discoverFleet: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d models, want 1 (only the healthy vLLM): %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "qwen3.6-35b-a3b" || e.UpstreamModel != "qwen3.6-35b-a3b" {
		t.Errorf("name/upstream wrong: %+v", e)
	}
	if e.URL != "http://127.0.0.1:8000/v1" {
		t.Errorf("URL = %q, want the /v1 base derived from the probe URL", e.URL)
	}
	if e.protocol() != protocolOpenAI {
		t.Errorf("protocol = %q, want openai", e.protocol())
	}
	if !e.isLocal() {
		t.Error("a discovered fleet model must be marked local")
	}
	if e.Key != "" {
		t.Errorf("a local fleet model must carry no key, got %q", e.Key)
	}
}

func TestDiscoverFleet_SendsTokenAndDedups(t *testing.T) {
	// Two nodes serve the same model id; the first wins. Server also enforces the
	// fleet token, proving discoverFleet authenticates.
	dup := `{"services":[
		{"node":"a","name":"vllm","url":"http://10.0.0.1:8000/v1/models","ok":true,"model":"shared","nodeReachable":true},
		{"node":"b","name":"vllm","url":"http://10.0.0.2:8000/v1/models","ok":true,"model":"shared","nodeReachable":true}
	]}`
	srv := fleetServer(t, "fleet-secret", dup)
	defer srv.Close()

	got, err := discoverFleet(context.Background(), srv.Client(), fleetSource{URL: srv.URL, Token: "fleet-secret"})
	if err != nil {
		t.Fatalf("discoverFleet: %v", err)
	}
	if len(got) != 1 || got[0].URL != "http://10.0.0.1:8000/v1" {
		t.Fatalf("dedup by model id failed (first node should win): %+v", got)
	}
}

func TestDiscoverFleet_AuthRejected(t *testing.T) {
	srv := fleetServer(t, "right-token", servicesPayload)
	defer srv.Close()
	// Wrong token → 401 → discoverFleet surfaces an error (caller keeps last-known).
	if _, err := discoverFleet(context.Background(), srv.Client(), fleetSource{URL: srv.URL, Token: "wrong"}); err == nil {
		t.Fatal("expected an error when the fleet token is rejected")
	}
}

func TestDeriveOpenAIBase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://127.0.0.1:8000/v1/models", "http://127.0.0.1:8000/v1"},
		{"http://127.0.0.1:8000/v1/models/", "http://127.0.0.1:8000/v1"},
		{"http://127.0.0.1:8000/v1", "http://127.0.0.1:8000/v1"},
		{"  http://127.0.0.1:8000/v1  ", "http://127.0.0.1:8000/v1"},
		{"http://127.0.0.1:18011/health", ""}, // sidecar, not an OpenAI endpoint
		{"", ""},
	}
	for _, c := range cases {
		if got := deriveOpenAIBase(c.in); got != c.want {
			t.Errorf("deriveOpenAIBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRouterLookup_ConfigWinsOverFleet(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "shared", URL: "http://config/v1", UpstreamModel: "shared"},
	}})
	// Stage a discovered set: "shared" (clashes with config) + "fleetonly".
	local := true
	fleet := map[string]modelEntry{
		"shared":    {Name: "shared", URL: "http://fleet/v1", UpstreamModel: "shared", Local: &local},
		"fleetonly": {Name: "fleetonly", URL: "http://fleet/v1", UpstreamModel: "fleetonly", Local: &local},
	}
	rt.fleet.Store(&fleet)

	if e, ok := rt.lookup("shared"); !ok || e.URL != "http://config/v1" {
		t.Errorf("config entry must win on a name clash, got %+v ok=%v", e, ok)
	}
	if e, ok := rt.lookup("fleetonly"); !ok || e.URL != "http://fleet/v1" {
		t.Errorf("fleet-only model must resolve from discovery, got %+v ok=%v", e, ok)
	}
	if _, ok := rt.lookup("nope"); ok {
		t.Error("unknown model must not resolve")
	}

	merged := rt.mergedModels()
	if len(merged) != 2 {
		t.Fatalf("mergedModels = %d, want 2 (shared deduped + fleetonly): %+v", len(merged), merged)
	}
	// "shared" must appear once, sourced from config.
	for _, e := range merged {
		if e.Name == "shared" && e.URL != "http://config/v1" {
			t.Errorf("merged 'shared' must be the config entry, got %q", e.URL)
		}
	}
}

// End-to-end: a model that exists ONLY in the discovered fleet set is routable
// through serve() — proving lookup() is wired into the request path.
func TestServe_RoutesToDiscoveredModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"from":"fleet"}`)
	}))
	defer upstream.Close()

	rt := quietRouter(config{})
	local := true
	fleet := map[string]modelEntry{
		"discovered": {Name: "discovered", URL: upstream.URL + "/v1", UpstreamModel: "discovered", Protocol: protocolOpenAI, Local: &local},
	}
	rt.fleet.Store(&fleet)

	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"discovered"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), `"from":"fleet"`) {
		t.Errorf("discovered model should route to its backend, got %q", out)
	}
}
