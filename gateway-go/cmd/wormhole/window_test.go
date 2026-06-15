package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestProbeMaxModelLen(t *testing.T) {
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
func TestRefreshWindows_LocalOnlyAndListed(t *testing.T) {
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
