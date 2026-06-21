package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestKeyHealthLabel(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		st    keyHealthState
		cloud bool
		want  string
	}{
		{"local model never has a label", keyHealthState{OK: true, Status: 200, CheckedAt: now}, false, ""},
		{"cloud unprobed", keyHealthState{}, true, "unchecked"},
		{"cloud ok", keyHealthState{OK: true, Status: 200, CheckedAt: now}, true, "ok"},
		{"cloud 401", keyHealthState{Status: 401, CheckedAt: now}, true, "auth_failed"},
		{"cloud 403", keyHealthState{Status: 403, CheckedAt: now}, true, "auth_failed"},
		{"cloud 429", keyHealthState{Status: 429, CheckedAt: now}, true, "rate_limited"},
		{"cloud unreachable", keyHealthState{Status: 0, CheckedAt: now}, true, "unreachable"},
		{"cloud other", keyHealthState{Status: 500, CheckedAt: now}, true, "http_500"},
	}
	for _, tc := range cases {
		if got := tc.st.label(tc.cloud); got != tc.want {
			t.Errorf("%s: label=%q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestProbeKeyAuth(t *testing.T) {
	cases := []struct {
		code      int
		wantOK    bool
		wantLabel string
	}{
		{200, true, "ok"},
		{401, false, "auth_failed"},
		{403, false, "auth_failed"},
		{429, false, "rate_limited"},
		{500, false, "http_500"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				t.Errorf("probe hit %q, want /chat/completions", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
				t.Errorf("auth header = %q, want Bearer sk-test", got)
			}
			w.WriteHeader(tc.code)
		}))
		e := modelEntry{Name: "glm", URL: srv.URL, Key: "sk-test"}
		st := probeKeyAuth(context.Background(), srv.Client(), e)
		if st.OK != tc.wantOK || st.label(true) != tc.wantLabel {
			t.Errorf("code %d: OK=%v label=%q, want OK=%v label=%q",
				tc.code, st.OK, st.label(true), tc.wantOK, tc.wantLabel)
		}
		srv.Close()
	}
}

// A dead endpoint records status 0 → "unreachable", never a false "ok".
func TestProbeKeyAuth_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // now refuses connections
	e := modelEntry{Name: "glm", URL: url, Key: "sk-test"}
	st := probeKeyAuth(context.Background(), &http.Client{Timeout: 2 * time.Second}, e)
	if st.OK || st.label(true) != "unreachable" {
		t.Errorf("closed endpoint: OK=%v label=%q, want OK=false label=unreachable", st.OK, st.label(true))
	}
}
