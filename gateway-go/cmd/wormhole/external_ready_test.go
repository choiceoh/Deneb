package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestForward_RetriesTransient5xx(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) <= 2 { // first 2 attempts 5xx, 3rd succeeds (1 + maxUpstreamRetries)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{{Name: "m", URL: upstream.URL + "/v1", UpstreamModel: "m"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("retry should reach the eventual 200, got %q (hits=%d)", body, atomic.LoadInt32(&hits))
	}
	if n := atomic.LoadInt32(&hits); n != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", n)
	}
}

func TestForward_ExhaustsPersistent5xx(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{{Name: "m", URL: upstream.URL + "/v1", UpstreamModel: "m"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if n := atomic.LoadInt32(&hits); n != 3 {
		t.Errorf("expected 3 attempts before giving up, got %d", n)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("the final 5xx should stream back, got %d", resp.StatusCode)
	}
}

func TestNoEffortRouting_Header(t *testing.T) {
	mk := func(v string) *http.Request {
		r, _ := http.NewRequest("POST", "/", nil)
		if v != "" {
			r.Header.Set("X-Wormhole-No-Effort", v)
		}
		return r
	}
	if noEffortRouting(mk("")) {
		t.Error("absent header should not opt out")
	}
	for _, v := range []string{"1", "true", "YES", "on"} {
		if !noEffortRouting(mk(v)) {
			t.Errorf("%q should opt out", v)
		}
	}
	if noEffortRouting(mk("0")) {
		t.Error("'0' should not opt out")
	}
}

// With the opt-out header, a smart client's body passes through byte-identical —
// wormhole skips effort routing even for a toggleKwarg model, so the gateway's own
// thinking decision (and its prefix cache) is never disturbed.
func TestEffortOptOut_PassesBodyThrough(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "m", URL: upstream.URL + "/v1", UpstreamModel: "m", ToggleKwarg: "thinking"}, // would route by effort
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	req := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	r, _ := http.NewRequest("POST", srv.URL+"/v1/chat/completions", strings.NewReader(req))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Wormhole-No-Effort", "1")
	_, _ = http.DefaultClient.Do(r)

	if got != req {
		t.Errorf("opt-out must pass the body through unchanged.\n got:  %s\n want: %s", got, req)
	}
	if strings.Contains(got, "chat_template_kwargs") {
		t.Error("opt-out must not inject chat_template_kwargs")
	}
}

func TestIsLoopbackListen(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:18800", true},
		{"localhost:18800", true},
		{"[::1]:18800", true},
		{":18800", false},              // all interfaces
		{"0.0.0.0:18800", false},       // all interfaces
		{"100.105.145.6:18800", false}, // tailnet
	}
	for _, c := range cases {
		if got := isLoopbackListen(c.in); got != c.want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
