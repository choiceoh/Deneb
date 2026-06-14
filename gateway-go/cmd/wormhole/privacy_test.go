package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsLocalURL(t *testing.T) {
	local := []string{
		"http://127.0.0.1:8000/v1", "http://localhost:8000", "http://10.0.0.5:8000",
		"http://192.168.1.10/v1", "http://172.16.3.4/v1", "http://[::1]:8000/v1",
	}
	cloud := []string{
		"https://openrouter.ai/api/v1", "https://api.anthropic.com", "http://8.8.8.8/v1",
	}
	for _, u := range local {
		if !isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = false, want local", u)
		}
	}
	for _, u := range cloud {
		if isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = true, want cloud", u)
		}
	}
}

func TestModelEntry_LocalOverride(t *testing.T) {
	f, tr := false, true
	if (modelEntry{URL: "http://127.0.0.1/v1", Local: &f}).isLocal() {
		t.Error("explicit local=false must override a loopback URL → cloud")
	}
	if !(modelEntry{URL: "https://public.example/v1", Local: &tr}).isLocal() {
		t.Error("explicit local=true must override a public URL → local")
	}
	if !(modelEntry{URL: "http://127.0.0.1/v1"}).isLocal() {
		t.Error("auto-detect: loopback URL should be local")
	}
	if (modelEntry{URL: "https://openrouter.ai/v1"}).isLocal() {
		t.Error("auto-detect: public URL should be cloud")
	}
}

func TestLocalOnly_Mode_BlocksCloudAllowsLocal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	rt := quietRouter(config{
		LocalOnly: true,
		Models: []modelEntry{
			{Name: "local", URL: upstream.URL + "/v1", UpstreamModel: "local"}, // 127.0.0.1 → local
			{Name: "cloud", URL: "https://api.example.com/v1", UpstreamModel: "x"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// Cloud model is refused outright — never reaches the upstream.
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"cloud"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cloud under local-only = %d, want 403", resp.StatusCode)
	}
	// Local model still forwards.
	resp2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"local"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("local under local-only = %d, want 200 (forwarded)", resp2.StatusCode)
	}
}

func TestLocalOnly_Header_BlocksCloud(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "cloud", URL: "https://api.example.com/v1", UpstreamModel: "x"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// With the header, a sensitive caller forces local-only → cloud is 403.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"cloud"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wormhole-Local-Only", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cloud with X-Wormhole-Local-Only = %d, want 403", resp.StatusCode)
	}
}
