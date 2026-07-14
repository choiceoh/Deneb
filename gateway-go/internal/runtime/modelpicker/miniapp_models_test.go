package modelpicker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildMiniappModelHealthAuthOverlayUpdatesStatus(t *testing.T) {
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

	got := buildMiniappModelHealth(sections, probes, nil)
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

	// A role-watch auth verdict overrides reachability: zai answers GET
	// /models with 200 even on an expired key, so the "online" above must
	// flip to "auth" once the 1-token probe has seen the 401.
	got = buildMiniappModelHealth(sections, probes, map[string]string{"zai": miniappModelHealthAuth})
	if got["zai/glm-5.1"] != miniappModelHealthAuth {
		t.Errorf("auth overlay: health[zai/glm-5.1] = %q, want %q", got["zai/glm-5.1"], miniappModelHealthAuth)
	}
	if got["vllm/qwen3.6-35b-a3b"] != miniappModelHealthOnline {
		t.Errorf("auth overlay must not leak to other providers: got %q", got["vllm/qwen3.6-35b-a3b"])
	}
}

func TestCapMergedModelsExemptsDeclaredTruncatesDiscovered(t *testing.T) {
	many := func(prefix string, n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, prefix+string(rune('a'+i)))
		}
		return out
	}

	// Declared models beyond the display cap all survive; the cap only
	// trims discovered extras appended after them.
	declared := many("d-", maxModelsPerProvider+2)
	got := capMergedModels(declared, many("x-", 3))
	if len(got) != len(declared) {
		t.Fatalf("declared exempt: got %d models, want all %d declared", len(got), len(declared))
	}
	for i, id := range declared {
		if got[i] != id {
			t.Fatalf("declared exempt: got[%d] = %q, want %q", i, got[i], id)
		}
	}

	// Few declared: discovered extras fill up to the cap, no further.
	got = capMergedModels([]string{"d-1", "d-2"}, many("x-", maxModelsPerProvider+5))
	if len(got) != maxModelsPerProvider {
		t.Fatalf("discovered capped: got %d models, want %d", len(got), maxModelsPerProvider)
	}
	if got[0] != "d-1" || got[1] != "d-2" {
		t.Fatalf("declared must lead the merged list, got %v", got[:2])
	}
}

// Regression: the discovered list doubles as the health-membership authority,
// so it must NOT be trimmed to the display cap — a served model beyond the cap
// rendered as a false "offline" while answering completions fine.
func TestDiscoverProviderModelsPreservesFullServedList(t *testing.T) {
	const served = maxModelsPerProvider + 8
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := `{"data":[`
		for i := 0; i < served; i++ {
			if i > 0 {
				payload += ","
			}
			payload += `{"id":"m` + string(rune('a'+i)) + `"}`
		}
		payload += `]}`
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	got := discoverProviderModels(context.Background(), srv.URL, "")
	if len(got) != served {
		t.Fatalf("discovered list truncated: got %d models, want %d", len(got), served)
	}
}

func TestEffectiveBaseURLResolvesKnownAndEmptyForUnknown(t *testing.T) {
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

func TestProbeModelsClassifiedWhenReachableAndListedVary(t *testing.T) {
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

func TestModelIDForProviderEntryPreservesNestedModelNames(t *testing.T) {
	entry := modelEntry{
		provider: "openrouter",
		fullID:   "openrouter/anthropic/claude-sonnet-4.6",
		display:  "claude-sonnet-4.6",
	}

	if got := modelIDForProviderEntry(entry); got != "anthropic/claude-sonnet-4.6" {
		t.Fatalf("modelIDForProviderEntry() = %q, want nested model id", got)
	}
}
