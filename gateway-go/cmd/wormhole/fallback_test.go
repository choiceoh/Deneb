package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFallback_UnreachablePrimaryFailsOver: a pinned model whose upstream is
// down (connection refused) must fail over to its declared fallback entry,
// with the body re-shaped for the fallback (model rewritten to ITS upstream
// id) — the 2026-07-06 dsv4-down scenario.
func TestFallback_UnreachablePrimaryFailsOver(t *testing.T) {
	var gotModel string
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		_, _ = io.WriteString(w, `{"from":"backup"}`)
	}))
	defer backup.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close() // now nothing listens — dial refused

	rt := quietRouter(config{
		Models: []modelEntry{
			{Name: "primary", URL: dead.URL + "/v1", UpstreamModel: "primary-up", Fallback: "backup"},
			{Name: "backup", URL: backup.URL + "/v1", UpstreamModel: "backup-up"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"primary"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), `"from":"backup"`) {
		t.Errorf("expected failover to backup, got status=%d body=%q", resp.StatusCode, out)
	}
	if gotModel != "backup-up" {
		t.Errorf("fallback body must be re-shaped to the fallback's upstream model, got %q", gotModel)
	}
}

// TestFallback_5xxPrimaryFailsOver: an upstream that keeps returning 5xx after
// the bounded retries moves to the fallback instead of streaming the 5xx.
func TestFallback_5xxPrimaryFailsOver(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"from":"backup"}`)
	}))
	defer backup.Close()

	rt := quietRouter(config{
		Models: []modelEntry{
			{Name: "primary", URL: primary.URL + "/v1", UpstreamModel: "primary", Fallback: "backup"},
			{Name: "backup", URL: backup.URL + "/v1", UpstreamModel: "backup"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"primary"}`))
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"from":"backup"`) {
		t.Errorf("expected 5xx failover to backup, got status=%d body=%q", resp.StatusCode, out)
	}
}

// TestFallback_UpstreamModelNotFoundFailsOver: once wormhole has accepted a
// configured route, an upstream 404 means that route's serving target is gone.
// The declared fallback can rescue it; the front-door unknown-model 404 remains
// covered separately by TestChatCompletions_UnknownModelIs404.
func TestFallback_UpstreamModelNotFoundFailsOver(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"from":"backup"}`)
	}))
	defer backup.Close()

	rt := quietRouter(config{
		Models: []modelEntry{
			{Name: "dsv4-nothink", URL: primary.URL + "/v1", UpstreamModel: "deepseek-v4-flash", Fallback: "glm-5.3"},
			{Name: "glm-5.3", URL: backup.URL + "/v1", UpstreamModel: "glm-5.3"},
		},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"dsv4-nothink"}`))
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"from":"backup"`) {
		t.Errorf("expected upstream 404 failover to backup, got status=%d body=%q", resp.StatusCode, out)
	}
}

// TestFallback_NoFallbackKeepsTodayBehavior: without a fallback, an
// unreachable upstream still surfaces as 502 "upstream unreachable".
func TestFallback_NoFallbackKeepsTodayBehavior(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	rt := quietRouter(config{
		Models: []modelEntry{{Name: "solo", URL: dead.URL + "/v1", UpstreamModel: "solo"}},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"solo"}`))
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(out), "upstream unreachable: solo") {
		t.Errorf("expected 502 upstream unreachable, got status=%d body=%q", resp.StatusCode, out)
	}
}

// TestFailoverChain_CycleAndGuards: chains are cycle-guarded and drop
// candidates that don't speak the primary's protocol.
func TestFailoverChain_CycleAndGuards(t *testing.T) {
	rt := quietRouter(config{
		Models: []modelEntry{
			{Name: "a", URL: "http://127.0.0.1:1/v1", Fallback: "b"},
			{Name: "b", URL: "http://127.0.0.1:1/v1", Fallback: "a"}, // cycle back to a
			{Name: "c", URL: "http://127.0.0.1:1/v1", Fallback: "anthro"},
			{Name: "anthro", URL: "http://127.0.0.1:1/v1", Protocol: protocolAnthropic},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	a, _ := rt.lookup("a")
	chain := rt.failoverChain(a, protocolOpenAI, req)
	if len(chain) != 2 || chain[1].Name != "b" {
		t.Errorf("cycle must stop after b, got %+v", chainNames(chain))
	}
	c, _ := rt.lookup("c")
	chain = rt.failoverChain(c, protocolOpenAI, req)
	if len(chain) != 1 {
		t.Errorf("protocol-mismatched fallback must be dropped, got %+v", chainNames(chain))
	}
}

func chainNames(chain []modelEntry) []string {
	names := make([]string, len(chain))
	for i, e := range chain {
		names[i] = e.Name
	}
	return names
}

// TestValidate_FallbackWarnings: load-time warnings for typo'd, self, and
// protocol-mismatched fallback targets.
func TestValidate_FallbackWarnings(t *testing.T) {
	warns := validateConfig(config{Models: []modelEntry{
		{Name: "a", URL: "http://x/v1", Fallback: "nope"},
		{Name: "b", URL: "http://x/v1", Fallback: "b"},
		{Name: "c", URL: "http://x/v1", Fallback: "anthro"},
		{Name: "anthro", URL: "http://x/v1", Protocol: protocolAnthropic},
	}})
	joined := strings.Join(warns, "\n")
	for _, want := range []string{
		"fallback nope is not a configured model",
		"declares itself as fallback",
		"speaks a different protocol",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in:\n%s", want, joined)
		}
	}
}
