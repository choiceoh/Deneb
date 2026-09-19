package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// vllmModelsSrv serves an OpenAI /v1/models payload reporting max_model_len, the
// shape a vLLM backend returns and wormhole probes for context windows.
func vllmModelsSrv(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeMaxModelLen_ReturnsWindowOrZero(t *testing.T) {
	srv := vllmModelsSrv(t, `{"data":[{"id":"dsv4","max_model_len":1000000}]}`)
	if got := probeMaxModelLen(context.Background(), http.DefaultClient, modelEntry{Name: "dsv4", URL: srv.URL + "/v1"}); got != 1000000 {
		t.Errorf("probe = %d, want 1000000", got)
	}
	// UpstreamModel wins over Name when set.
	if got := probeMaxModelLen(context.Background(), http.DefaultClient, modelEntry{Name: "alias", UpstreamModel: "dsv4", URL: srv.URL + "/v1"}); got != 1000000 {
		t.Errorf("probe by upstreamModel = %d, want 1000000", got)
	}
	// A model the backend doesn't serve → 0 (not a wrong window).
	if got := probeMaxModelLen(context.Background(), http.DefaultClient, modelEntry{Name: "nope", URL: srv.URL + "/v1"}); got != 0 {
		t.Errorf("probe for unserved model = %d, want 0", got)
	}
	// Unreachable backend → 0, no panic.
	if got := probeMaxModelLen(context.Background(), http.DefaultClient, modelEntry{Name: "dsv4", URL: "http://127.0.0.1:1/v1"}); got != 0 {
		t.Errorf("probe of closed port = %d, want 0", got)
	}
}

// refreshWindows probes only LOCAL openai backends and surfaces the window in
// /v1/models; a cloud-fronted model is left out (max_model_len isn't its fact).
func TestRefreshWindows_LoadsLocalWindowsIntoModelsList(t *testing.T) {
	srv := vllmModelsSrv(t, `{"data":[{"id":"dsv4","max_model_len":1000000}]}`)
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "dsv4", URL: srv.URL + "/v1"},                // local (loopback) vLLM
		{Name: "cloudy", URL: "https://api.example.com/v1"}, // cloud: must be skipped, never probed
	}})
	rt.refreshWindows(context.Background())

	w := rt.windows.Load()
	if w == nil || (*w)["dsv4"] != 1000000 {
		t.Fatalf("dsv4 window = %v, want 1000000", w)
	}
	if _, ok := (*w)["cloudy"]; ok {
		t.Errorf("cloud model must have no probed window, got %d", (*w)["cloudy"])
	}

	// /v1/models echoes the discovered window (no token configured → open).
	srvHTTP := httptest.NewServer(rt.handler())
	t.Cleanup(srvHTTP.Close)
	resp, err := http.Get(srvHTTP.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range out.Data {
		got[m.ID] = m.MaxModelLen
	}
	if got["dsv4"] != 1000000 {
		t.Errorf("/v1/models dsv4 max_model_len = %d, want 1000000", got["dsv4"])
	}
	if got["cloudy"] != 0 {
		t.Errorf("/v1/models cloudy max_model_len = %d, want 0 (omitted)", got["cloudy"])
	}
}

// TestRefreshWindows_FlagsAnUpstreamTheBackendDoesNotServe pins the difference
// between "could not ask" and "asked, and it is not there". The second is the
// stale-entry shape sidecar-models.md records as a paid-API leak that went
// twelve days unnoticed: wormhole already probed /v1/models and already knew.
func TestRefreshWindows_FlagsAnUpstreamTheBackendDoesNotServe(t *testing.T) {
	srv := vllmModelsSrv(t, `{"data":[{"id":"glm-5.3-flash","max_model_len":1048576}]}`)
	local := true
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "glm-5.3-flash-local", URL: srv.URL + "/v1", UpstreamModel: "glm-5.3-flash", Local: &local},
		{Name: "dsv4-nothink", URL: srv.URL + "/v1", UpstreamModel: "deepseek-v4-flash", Local: &local},
	}})

	rt.refreshWindows(context.Background())

	if w := (*rt.windows.Load())["glm-5.3-flash-local"]; w != 1048576 {
		t.Errorf("served entry window = %d, want 1048576", w)
	}
	missing := *rt.missingUpstream.Load()
	if !missing["dsv4-nothink"] {
		t.Error("an entry whose upstreamModel the backend does not serve must be flagged")
	}
	if missing["glm-5.3-flash-local"] {
		t.Error("a served entry must not be flagged")
	}
}

// A backend we cannot reach is NOT a stale entry: saying so would cry wolf on
// every restart and during every deploy.
func TestRefreshWindows_AnUnreachableBackendIsNotFlagged(t *testing.T) {
	local := true
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "down", URL: "http://127.0.0.1:1/v1", UpstreamModel: "whatever", Local: &local},
	}})

	rt.refreshWindows(context.Background())

	if (*rt.missingUpstream.Load())["down"] {
		t.Error("unreachable must not be reported as not-served")
	}
}

// The window probe records what the backend says about image input for the
// model each entry asks for, and the image gate follows it: the same entry is
// stripped while its backend boots text-only and passes once it reports the
// tower — without a config edit or a restart.
func TestRefreshWindows_TheImageGateFollowsTheBackendsReport(t *testing.T) {
	var takes atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{
			"id": "qwen3.8-flash-next", "max_model_len": 262144,
			"capabilities": map[string]any{"vision": takes.Load()},
		}}})
	}))
	t.Cleanup(srv.Close)
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "qwen", UpstreamModel: "qwen3.8-flash-next", URL: srv.URL + "/v1"},
		{Name: "glm", UpstreamModel: "glm-5.3-flash", URL: srv.URL + "/v1"}, // not served: no report
	}})
	withImage := []byte(`{"model":"qwen","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)
	entry := modelEntry{Name: "qwen", UpstreamModel: "qwen3.8-flash-next"}

	rt.refreshWindows(context.Background())
	if report := *rt.visionReport.Load(); report["qwen"] != false || len(report) != 1 {
		t.Fatalf("report = %v, want only qwen=false", report)
	}
	if out := rt.applyVisionGate(entry, withImage, protocolOpenAI); strings.Contains(string(out), "image_url") {
		t.Errorf("a text-only boot got the image: %s", out)
	}

	takes.Store(true)
	rt.refreshWindows(context.Background())
	if out := rt.applyVisionGate(entry, withImage, protocolOpenAI); string(out) != string(withImage) {
		t.Errorf("a boot with its tower lost the image: %s", out)
	}
}
