package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildMiniappModelHealth(t *testing.T) {
	sections := []modelSection{{
		title: "models",
		entries: []modelEntry{
			// listed provider: membership decides online/offline.
			{provider: "vllm", fullID: "vllm/qwen3.6-35b-a3b", display: "qwen3.6-35b-a3b"},
			{provider: "vllm", fullID: "vllm/missing-model", display: "missing-model"},
			{provider: "openrouter", fullID: "openrouter/anthropic/claude-sonnet-4.6", display: "claude-sonnet-4.6"},
			// reachable but not enumerable (e.g. Anthropic-format /models) → online.
			{provider: "zai", fullID: "zai/glm-5.1", display: "glm-5.1"},
			// probed but unreachable → offline.
			{provider: "kimi", fullID: "kimi/kimi-for-coding", display: "kimi-for-coding"},
			// never probed (no entry) → unknown.
			{provider: "mimo-plan", fullID: "mimo-plan/mimo-v2.5-pro", display: "mimo-v2.5-pro"},
		},
	}}
	probes := map[string]providerModelProbe{
		"vllm": {
			checked:   true,
			reachable: true,
			listed:    true,
			models:    []string{"qwen3.6-35b-a3b"},
		},
		"openrouter": {
			checked:   true,
			reachable: true,
			listed:    true,
			models:    []string{"anthropic/claude-sonnet-4.6"},
		},
		"zai":  {checked: true, reachable: true, listed: false},
		"kimi": {checked: true, reachable: false, listed: false},
	}

	got := buildMiniappModelHealth(sections, probes)
	want := map[string]string{
		"vllm/qwen3.6-35b-a3b":                   miniappModelHealthOnline,
		"vllm/missing-model":                     miniappModelHealthOffline,
		"openrouter/anthropic/claude-sonnet-4.6": miniappModelHealthOnline,
		"zai/glm-5.1":                            miniappModelHealthOnline,
		"kimi/kimi-for-coding":                   miniappModelHealthOffline,
		"mimo-plan/mimo-v2.5-pro":                miniappModelHealthUnknown,
	}
	for modelID, status := range want {
		if got[modelID] != status {
			t.Errorf("health[%q] = %q, want %q", modelID, got[modelID], status)
		}
	}
}

func TestEffectiveBaseURLResolvesCloudProviders(t *testing.T) {
	// Built-in cloud providers must resolve to a non-empty endpoint so the
	// health probe can reach them (otherwise their dots stay "unknown").
	for _, name := range []string{"zai", "openrouter", "kimi", "mimo-plan", "vllm", "localai"} {
		if got := effectiveBaseURL(providerSpec{name: name}); got == "" {
			t.Errorf("effectiveBaseURL(%q) = empty, want a default endpoint", name)
		}
	}
	// A configured base URL always wins.
	if got := effectiveBaseURL(providerSpec{name: "zai", baseURL: "http://example/v1"}); got != "http://example/v1" {
		t.Errorf("configured baseURL not honored: %q", got)
	}
	// Truly unknown providers stay empty (no probe → unknown dot).
	if got := effectiveBaseURL(providerSpec{name: "totally-unknown"}); got != "" {
		t.Errorf("effectiveBaseURL(unknown) = %q, want empty", got)
	}
}

func TestProbeModelsClassified(t *testing.T) {
	t.Run("200 with OpenAI list → listed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m1"},{"id":"m2"}]}`))
		}))
		defer srv.Close()
		models, listed, reachable := probeModelsClassified(context.Background(), srv.URL+"/v1")
		if !reachable || !listed {
			t.Fatalf("reachable=%v listed=%v, want both true", reachable, listed)
		}
		if len(models) != 2 || models[0] != "m1" || models[1] != "m2" {
			t.Errorf("models = %v, want [m1 m2]", models)
		}
	})

	t.Run("404 → reachable but not listed (Anthropic-format endpoint)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		models, listed, reachable := probeModelsClassified(context.Background(), srv.URL+"/anthropic")
		if !reachable {
			t.Error("reachable = false, want true (endpoint answered)")
		}
		if listed || len(models) != 0 {
			t.Errorf("listed=%v models=%v, want unlisted/empty", listed, models)
		}
	})

	t.Run("200 with non-OpenAI body → reachable, not listed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"models":["x"]}`)) // no top-level "data"
		}))
		defer srv.Close()
		_, listed, reachable := probeModelsClassified(context.Background(), srv.URL+"/v1")
		if !reachable || listed {
			t.Errorf("reachable=%v listed=%v, want reachable=true listed=false", reachable, listed)
		}
	})

	t.Run("connection refused → unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close() // nothing listening now
		_, listed, reachable := probeModelsClassified(context.Background(), url+"/v1")
		if reachable || listed {
			t.Errorf("reachable=%v listed=%v, want both false", reachable, listed)
		}
	})
}

func TestModelIDForProviderEntryKeepsNestedModelNames(t *testing.T) {
	entry := modelEntry{
		provider: "openrouter",
		fullID:   "openrouter/anthropic/claude-sonnet-4.6",
		display:  "claude-sonnet-4.6",
	}

	if got := modelIDForProviderEntry(entry); got != "anthropic/claude-sonnet-4.6" {
		t.Fatalf("modelIDForProviderEntry() = %q, want nested model id", got)
	}
}
