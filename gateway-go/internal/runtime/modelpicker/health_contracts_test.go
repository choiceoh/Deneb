package modelpicker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func TestAssembleMiniappModelSectionsDeduplicatesAcrossSources(t *testing.T) {
	t.Parallel()
	roles := []modelEntry{
		{
			provider: "zai",
			fullID:   "zai/glm",
			label:    "main",
			display:  "glm",
		},
		{
			provider: "vllm",
			fullID:   "vllm/local",
			label:    "coding",
			display:  "local",
		},
		{
			provider: "",
			fullID:   "",
			label:    "empty",
		},
	}
	providers := []providerSpec{
		{
			name:   "zai",
			models: []string{"glm", "other"},
		},
		{
			name:   "vllm",
			models: []string{"local", "second"},
		},
		{
			name:   "empty",
			models: nil,
		},
	}
	got := assembleMiniappModelSections(roles, providers)
	if len(got) != 3 {
		t.Fatalf("sections = %#v", got)
	}
	if got[0].title != "역할" || len(got[0].entries) != 2 {
		t.Fatalf("role section = %#v", got[0])
	}
	if got[1].title != "Z.ai" || len(got[1].entries) != 1 || got[1].entries[0].fullID != "zai/other" {
		t.Fatalf("zai section = %#v", got[1])
	}
	if got[2].title != "vLLM" || len(got[2].entries) != 1 || got[2].entries[0].fullID != "vllm/second" {
		t.Fatalf("vllm section = %#v", got[2])
	}
}

func TestMiniappModelHealthWhenProbeAndVerdictVary(t *testing.T) {
	t.Parallel()
	entry := modelEntry{
		provider: "zai",
		fullID:   "zai/glm",
		display:  "glm",
	}
	cases := []struct {
		name     string
		entry    modelEntry
		probes   map[string]providerModelProbe
		verdicts map[string]string
		want     string
	}{
		{
			name:  "empty-provider",
			entry: modelEntry{},
			want:  miniappModelHealthUnknown,
		},
		{
			name:  "missing-probe",
			entry: entry,
			want:  miniappModelHealthUnknown,
		},
		{
			name:   "unchecked",
			entry:  entry,
			probes: map[string]providerModelProbe{"zai": {}},
			want:   miniappModelHealthUnknown,
		},
		{
			name:  "auth-overrides-online",
			entry: entry,
			probes: map[string]providerModelProbe{
				"zai": {
					checked:   true,
					reachable: true,
					listed:    true,
					models:    []string{"glm"},
				},
			},
			verdicts: map[string]string{"zai": miniappModelHealthAuth},
			want:     miniappModelHealthAuth,
		},
		{
			name:  "listed-present",
			entry: entry,
			probes: map[string]providerModelProbe{
				"zai": {
					checked:   true,
					reachable: true,
					listed:    true,
					models:    []string{"glm"},
				},
			},
			want: miniappModelHealthOnline,
		},
		{
			name:  "listed-absent",
			entry: entry,
			probes: map[string]providerModelProbe{
				"zai": {
					checked:   true,
					reachable: true,
					listed:    true,
					models:    []string{"other"},
				},
			},
			want: miniappModelHealthOffline,
		},
		{
			name:  "reachable-unlisted",
			entry: entry,
			probes: map[string]providerModelProbe{
				"zai": {
					checked:   true,
					reachable: true,
				},
			},
			want: miniappModelHealthOnline,
		},
		{
			name:  "unreachable",
			entry: entry,
			probes: map[string]providerModelProbe{
				"zai": {
					checked: true,
				},
			},
			want: miniappModelHealthOffline,
		},
		{
			name: "nested-present",
			entry: modelEntry{
				provider: "openrouter",
				fullID:   "openrouter/anthropic/opus",
				display:  "opus",
			},
			probes: map[string]providerModelProbe{
				"openrouter": {
					checked:   true,
					reachable: true,
					listed:    true,
					models:    []string{"anthropic/opus"},
				},
			},
			want: miniappModelHealthOnline,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := miniappModelHealthForEntry(tc.entry, tc.probes, tc.verdicts); got != tc.want {
				t.Fatalf("health = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProbeModelsClassifiedHandlesMalformedResponses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantModels []string
		listed     bool
		reachable  bool
	}{
		{
			name:       "one",
			status:     http.StatusOK,
			body:       "{\"data\":[{\"id\":\"alpha\"}]}",
			wantModels: []string{"alpha"},
			listed:     true,
			reachable:  true,
		},
		{
			name:       "trim-and-drop",
			status:     http.StatusOK,
			body:       "{\"data\":[{\"id\":\" alpha \"},{\"id\":\"\"},{\"id\":\"beta\"}]}",
			wantModels: []string{"alpha", "beta"},
			listed:     true,
			reachable:  true,
		},
		{
			name:      "empty-list",
			status:    http.StatusOK,
			body:      "{\"data\":[]}",
			reachable: true,
		},
		{
			name:      "missing-data",
			status:    http.StatusOK,
			body:      "{}",
			reachable: true,
		},
		{
			name:      "malformed",
			status:    http.StatusOK,
			body:      "{",
			reachable: true,
		},
		{
			name:      "unauthorized",
			status:    http.StatusUnauthorized,
			body:      "{\"error\":\"no\"}",
			reachable: true,
		},
		{
			name:      "not-found",
			status:    http.StatusNotFound,
			body:      "missing",
			reachable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models" {
					t.Errorf("path = %q", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			models, listed, reachable := probeModelsClassified(context.Background(), srv.URL+"/")
			if !reflect.DeepEqual(models, tc.wantModels) || listed != tc.listed || reachable != tc.reachable {
				t.Fatalf("got (%#v, %v, %v), want (%#v, %v, %v)", models, listed, reachable, tc.wantModels, tc.listed, tc.reachable)
			}
		})
	}
}

// A local (loopback) provider that local discovery cannot enumerate — an
// Anthropic-front route on the wormhole router has no OpenAI /models — must
// fall back to the plain reachability probe instead of reading as a false
// "offline". A genuinely dead local endpoint must still probe unreachable.
func TestMiniappModelHealthProbes_LocalAnthropicFrontFallsBackToReachability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // wormhole loopback: /models does not exist on the anthropic front
	}))
	defer srv.Close()

	s := &Controller{}
	probes := s.miniappModelHealthProbes(context.Background(), []providerSpec{
		{name: "kimi", baseURL: srv.URL},
		{name: "dead", baseURL: "http://127.0.0.1:1"},
		{name: "vllm", baseURL: srv.URL},
	}, map[string][]string{"vllm": {"served-model"}})

	if p := probes["kimi"]; !p.checked || !p.reachable || p.listed {
		t.Fatalf("kimi probe = %+v, want checked+reachable without a list", p)
	}
	if p := probes["dead"]; !p.checked || p.reachable {
		t.Fatalf("dead probe = %+v, want checked+unreachable", p)
	}
	if p := probes["vllm"]; !p.listed || !p.reachable || len(p.models) != 1 {
		t.Fatalf("vllm probe = %+v, want discovery list preserved", p)
	}
}

func TestProbeModelsClassifiedHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	models, listed, reachable := probeModelsClassified(ctx, "http://127.0.0.1:1")
	if models != nil || listed || reachable {
		t.Fatalf("canceled probe = (%#v, %v, %v)", models, listed, reachable)
	}
}

func TestBuildMiniappModelHealthConcurrentReadSafety(t *testing.T) {
	t.Parallel()
	sections := []modelSection{
		{
			title: "models",
			entries: []modelEntry{
				{provider: "p", fullID: "p/a", display: "a"},
				{provider: "p", fullID: "p/b", display: "b"},
			},
		},
	}
	probes := map[string]providerModelProbe{
		"p": {
			checked:   true,
			reachable: true,
			listed:    true,
			models:    []string{"a", "b"},
		},
	}
	const workers = 32
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got := buildMiniappModelHealth(sections, probes, nil)
				if got["p/a"] != miniappModelHealthOnline || got["p/b"] != miniappModelHealthOnline {
					errs <- fmt.Errorf("bad health: %#v", got)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestProviderModelProbeEncodesToEmptyJSON(t *testing.T) {
	t.Parallel()
	probe := providerModelProbe{
		checked:   true,
		reachable: true,
		listed:    true,
		models:    []string{"a"},
	}
	data, err := json.Marshal(probe) //nolint:staticcheck // contract under test: probe must serialize to {} (nothing leaks)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Fatalf("unexported probe fields leaked to JSON: %s", data)
	}
}
