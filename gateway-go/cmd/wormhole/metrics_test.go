package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetrics_RecordAndPrometheus(t *testing.T) {
	m := newMetrics()
	m.record("dsv4", "deneb", 200, 100*time.Millisecond)
	m.record("dsv4", "deneb", 200, 300*time.Millisecond)
	m.record("dsv4", "claude-code", 500, 50*time.Millisecond) // error
	m.record("claude", "curl", 401, 10*time.Millisecond)      // error
	m.record("", "", 200, 5*time.Millisecond)                 // unnamed -> "(none)" / "unknown"

	var b strings.Builder
	m.writePrometheus(&b)
	out := b.String()

	for _, want := range []string{
		"wormhole_requests_total 5",
		"wormhole_request_errors_total 2",
		`wormhole_model_requests_total{model="dsv4"} 3`,
		`wormhole_model_errors_total{model="dsv4"} 1`,
		`wormhole_model_latency_ms_sum{model="dsv4"} 450`,
		`wormhole_model_requests_total{model="claude"} 1`,
		`wormhole_model_errors_total{model="claude"} 1`,
		`wormhole_model_requests_total{model="(none)"} 1`,
		`wormhole_client_requests_total{client="deneb"} 2`,
		`wormhole_client_requests_total{client="claude-code"} 1`,
		`wormhole_client_requests_total{client="curl"} 1`,
		`wormhole_client_requests_total{client="unknown"} 1`, // empty client -> unknown
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// A real request through the router increments metrics and /metrics serves them —
// proving the serve() instrumentation captures the forwarded upstream status.
func TestServe_RecordsMetricsAndExposes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "m1", URL: upstream.URL + "/v1", UpstreamModel: "m1"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// One successful request + one to an unknown model (404, an error).
	_, _ = http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m1"}`))
	_, _ = http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"nope"}`))

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	out := string(body)
	if !strings.Contains(out, "wormhole_requests_total 2") {
		t.Errorf("want 2 total requests recorded; got:\n%s", out)
	}
	if !strings.Contains(out, `wormhole_model_requests_total{model="m1"} 1`) {
		t.Errorf("m1 request not recorded:\n%s", out)
	}
	// The unknown-model request is a 404 -> error, recorded under "nope".
	if !strings.Contains(out, `wormhole_model_errors_total{model="nope"} 1`) {
		t.Errorf("404 for unknown model not recorded as an error:\n%s", out)
	}
}

func TestMetrics_TokenGated(t *testing.T) {
	rt := quietRouter(config{Token: "sekret", Models: []modelEntry{{Name: "m", URL: "http://x/v1", UpstreamModel: "m"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/metrics")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/metrics without token = %d, want 401", resp.StatusCode)
	}
}
