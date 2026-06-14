package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatus_MergesConfigAndFleetWithSource(t *testing.T) {
	rt := quietRouter(config{
		Token:     "sekret",
		LocalOnly: false,
		Auto:      []string{"dsv4"},
		Models: []modelEntry{
			{Name: "dsv4", URL: "http://127.0.0.1:8000/v1", UpstreamModel: "dsv4", ToggleKwarg: "thinking"},
			{Name: "claude", URL: "https://api.anthropic.com/v1", Protocol: "anthropic", Key: "leak-me", UpstreamModel: "claude"},
		},
	})
	local := true
	fleet := map[string]modelEntry{
		"qwen": {Name: "qwen", URL: "http://127.0.0.1:8003/v1", UpstreamModel: "qwen", Protocol: protocolOpenAI, Local: &local},
	}
	rt.fleet.Store(&fleet)

	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// Token-gated: no token → 401.
	noauth, _ := http.Get(srv.URL + "/status")
	if noauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/status without token = %d, want 401", noauth.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "leak-me") {
		t.Fatal("an upstream key leaked into /status")
	}

	var out statusOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.EffortRouting || out.LocalOnly {
		t.Errorf("flags: effortRouting=%v localOnly=%v, want true/false", out.EffortRouting, out.LocalOnly)
	}
	byName := map[string]statusModelRow{}
	for _, m := range out.Models {
		byName[m.Name] = m
	}
	if len(out.Models) != 3 {
		t.Fatalf("models = %d, want 3 (2 config + 1 fleet): %+v", len(out.Models), out.Models)
	}
	if m := byName["dsv4"]; m.Source != "config" || !m.Local || !m.Thinking || m.Protocol != "openai" {
		t.Errorf("dsv4 row wrong: %+v", m)
	}
	if m := byName["claude"]; m.Source != "config" || m.Local || m.Protocol != "anthropic" {
		t.Errorf("claude row wrong: %+v", m)
	}
	if m := byName["qwen"]; m.Source != "fleet" || !m.Local || m.Thinking || m.Protocol != "openai" {
		t.Errorf("qwen (discovered) row wrong: %+v (want source=fleet, local, no thinking)", m)
	}
}

// A configured model shadows a discovered one of the same name: /status shows it
// once, sourced from config.
func TestStatus_ConfigShadowsFleet(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "shared", URL: "http://config/v1", UpstreamModel: "shared"},
	}})
	local := true
	fleet := map[string]modelEntry{
		"shared": {Name: "shared", URL: "http://fleet/v1", UpstreamModel: "shared", Local: &local},
	}
	rt.fleet.Store(&fleet)

	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/status") // no token configured → open
	var out statusOut
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Models) != 1 || out.Models[0].Source != "config" {
		t.Fatalf("shadowed model must appear once from config: %+v", out.Models)
	}
}
