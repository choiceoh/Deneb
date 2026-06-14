package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessages_ForwardsToAnthropicBackend(t *testing.T) {
	var gotKey, gotVer, gotPath, gotModel, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotModel = extractModel(b)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "claude", URL: upstream.URL + "/v1", Key: "sk-ant", Protocol: "anthropic", UpstreamModel: "claude-opus-4"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	body := `{"model":"claude","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	if gotPath != "/v1/messages" {
		t.Errorf("upstream path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-ant" {
		t.Errorf("x-api-key = %q, want injected sk-ant", gotKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty for an anthropic backend (x-api-key only)", gotAuth)
	}
	if gotVer == "" {
		t.Error("anthropic-version not set — Anthropic requires it")
	}
	if gotModel != "claude-opus-4" {
		t.Errorf("upstream model = %q, want rewritten claude-opus-4", gotModel)
	}
	if !strings.Contains(string(out), "message_stop") {
		t.Errorf("anthropic stream not passed through: %q", out)
	}
}

func TestProtocolMismatch_IsActionable400(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "oai", URL: "http://127.0.0.1:1/v1", Protocol: "openai", UpstreamModel: "oai"},
		{Name: "ant", URL: "http://127.0.0.1:1/v1", Protocol: "anthropic", UpstreamModel: "ant"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// An OpenAI model on the Anthropic endpoint → 400 (no translation).
	r1, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"oai"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r1.StatusCode != http.StatusBadRequest {
		t.Errorf("openai model on /v1/messages = %d, want 400", r1.StatusCode)
	}
	// An Anthropic model on the OpenAI endpoint → 400.
	r2, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"ant"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r2.StatusCode != http.StatusBadRequest {
		t.Errorf("anthropic model on /v1/chat/completions = %d, want 400", r2.StatusCode)
	}
}

func TestClientToken_AcceptsXApiKey(t *testing.T) {
	rt := quietRouter(config{Token: "sekret", Models: []modelEntry{
		{Name: "ant", URL: "http://127.0.0.1:1/v1", Protocol: "anthropic", UpstreamModel: "ant"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// Correct token via x-api-key (the Anthropic convention) → passes auth (then
	// fails at the unreachable upstream with 502, NOT 401).
	good, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", strings.NewReader(`{"model":"ant"}`))
	good.Header.Set("x-api-key", "sekret")
	respGood, err := http.DefaultClient.Do(good)
	if err != nil {
		t.Fatal(err)
	}
	if respGood.StatusCode == http.StatusUnauthorized {
		t.Error("x-api-key with the right token should authenticate, got 401")
	}
	// Wrong token → 401.
	bad, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", strings.NewReader(`{"model":"ant"}`))
	bad.Header.Set("x-api-key", "nope")
	respBad, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	if respBad.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong x-api-key = %d, want 401", respBad.StatusCode)
	}
}
