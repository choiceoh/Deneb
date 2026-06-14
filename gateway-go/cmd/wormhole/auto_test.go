package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuto_FirstHealthyWins(t *testing.T) {
	var bHit bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"from":"a"}`)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { bHit = true }))
	defer b.Close()

	rt := quietRouter(config{
		Auto: []string{"a", "b"},
		Models: []modelEntry{
			{Name: "a", URL: a.URL + "/v1", UpstreamModel: "a"},
			{Name: "b", URL: b.URL + "/v1", UpstreamModel: "b"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"auto"}`))
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), `"from":"a"`) {
		t.Errorf("first healthy candidate should win, got %q", out)
	}
	if bHit {
		t.Error("b should not be tried when a is healthy")
	}
}

func TestAuto_FallsBackOn5xx(t *testing.T) {
	var aHit, bHit bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aHit = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bHit = true
		_, _ = io.WriteString(w, `{"from":"b"}`)
	}))
	defer b.Close()

	rt := quietRouter(config{
		Auto: []string{"a", "b"},
		Models: []modelEntry{
			{Name: "a", URL: a.URL + "/v1", UpstreamModel: "a"},
			{Name: "b", URL: b.URL + "/v1", UpstreamModel: "b"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"auto"}`))
	out, _ := io.ReadAll(resp.Body)
	if !aHit || !bHit {
		t.Errorf("expected a(500) then fallback to b; aHit=%v bHit=%v", aHit, bHit)
	}
	if !strings.Contains(string(out), `"from":"b"`) {
		t.Errorf("auto should have fallen back to b, got %q", out)
	}
}

func TestAuto_EgressGuardSkipsCloud(t *testing.T) {
	var localHit bool
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		localHit = true
		_, _ = io.WriteString(w, `{"from":"local"}`)
	}))
	defer local.Close()

	// cloud is first in the auto list, but a local-only instance must skip it and
	// pick the local candidate.
	rt := quietRouter(config{
		LocalOnly: true,
		Auto:      []string{"cloud", "local"},
		Models: []modelEntry{
			{Name: "cloud", URL: "https://api.example.com/v1", UpstreamModel: "x"},
			{Name: "local", URL: local.URL + "/v1", UpstreamModel: "local"}, // 127.0.0.1 → local
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"auto"}`))
	out, _ := io.ReadAll(resp.Body)
	if !localHit || !strings.Contains(string(out), `"from":"local"`) {
		t.Errorf("local-only auto should skip cloud and pick local; localHit=%v out=%q", localHit, out)
	}
}

func TestAuto_ProtocolFiltering(t *testing.T) {
	var oaiHit, antHit bool
	oai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { oaiHit = true }))
	defer oai.Close()
	ant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		antHit = true
		_, _ = io.WriteString(w, "ok")
	}))
	defer ant.Close()

	rt := quietRouter(config{
		Auto: []string{"oai", "ant"},
		Models: []modelEntry{
			{Name: "oai", URL: oai.URL + "/v1", Protocol: "openai", UpstreamModel: "oai"},
			{Name: "ant", URL: ant.URL + "/v1", Protocol: "anthropic", UpstreamModel: "ant"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// auto on the Anthropic endpoint must only consider anthropic candidates.
	_, _ = http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"auto"}`))
	if oaiHit {
		t.Error("openai candidate must be skipped for an /v1/messages auto request")
	}
	if !antHit {
		t.Error("anthropic candidate should serve the /v1/messages auto request")
	}
}

func TestAuto_AllFailIs502(t *testing.T) {
	rt := quietRouter(config{
		Auto: []string{"a", "b"},
		Models: []modelEntry{
			{Name: "a", URL: "http://127.0.0.1:1/v1", UpstreamModel: "a"},
			{Name: "b", URL: "http://127.0.0.1:1/v1", UpstreamModel: "b"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"auto"}`))
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("all-unreachable auto = %d, want 502", resp.StatusCode)
	}
}

func TestAuto_AdvertisedInModels(t *testing.T) {
	rt := quietRouter(config{Auto: []string{"a"}, Models: []modelEntry{{Name: "a", URL: "http://x/v1", UpstreamModel: "a"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/v1/models")
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), `"auto"`) {
		t.Errorf("/v1/models should advertise the auto name, got %q", out)
	}
}
