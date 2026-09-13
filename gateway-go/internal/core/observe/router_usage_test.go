package observe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchRouterUsageReadsTheMeterAndSortsByRequests(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(RouterUsage{
			Window: "2026-09",
			Models: []RouterModelUsage{
				{Model: "glm-5.3-flash-local", Requests: 838, InputTokens: 20_372_153},
				{Model: "glm-5.3-flash", Requests: 4412, InputTokens: 156_517_452},
			},
		})
	}))
	defer srv.Close()

	usage, ok := FetchRouterUsage(context.Background(), srv.URL, "tok")
	if !ok {
		t.Fatal("meter read failed")
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if usage.Window != "2026-09" || len(usage.Models) != 2 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.Models[0].Model != "glm-5.3-flash" {
		t.Errorf("rows not sorted by requests: %+v", usage.Models)
	}
}

// The split is the whole point: both entries answer under the same model name,
// so only the entry NAME says which one served.
func TestLocalShareSplitsByEntryName(t *testing.T) {
	usage := RouterUsage{Models: []RouterModelUsage{
		{Model: "glm-5.3-flash-local", Requests: 838},
		{Model: "glm-5.3-flash-local-low", Requests: 1618},
		{Model: "glm-5.3-flash", Requests: 4412},
		{Model: "k3", Requests: 3767},
	}}
	local, remote := usage.LocalShare(map[string]bool{
		"glm-5.3-flash-local": true, "glm-5.3-flash-local-low": true,
	})
	if local != 2456 || remote != 8179 {
		t.Errorf("local=%d remote=%d, want 2456/8179", local, remote)
	}
	// An unknown set must not credit anything to the local engine.
	if l, r := usage.LocalShare(nil); l != 0 || r != 10635 {
		t.Errorf("nil local set: local=%d remote=%d", l, r)
	}
}

func TestRouterUsageRefusesAnythingButAnOwnedHost(t *testing.T) {
	for _, in := range []string{
		"", "not a url", "ftp://127.0.0.1",
		"https://api.openai.com/v1",
		"http://8.8.8.8:18800/v1",
		"http://user:pass@127.0.0.1:18800/v1",
		"http://127.0.0.1:18800/v1?x=1",
	} {
		if got := routerUsageURL(in); got != "" {
			t.Errorf("routerUsageURL(%q) = %q, want \"\"", in, got)
		}
	}
	for _, in := range []string{"http://127.0.0.1:18800", "http://127.0.0.1:18800/v1", "http://localhost:18800/"} {
		if got := routerUsageURL(in); got == "" || got[len(got)-len("/v1/usage"):] != "/v1/usage" {
			t.Errorf("routerUsageURL(%q) = %q", in, got)
		}
	}
}

func TestFetchRouterUsageMissesOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, ok := FetchRouterUsage(context.Background(), srv.URL, ""); ok {
		t.Error("a rejected token must read as unavailable, not as zero usage")
	}
	if _, ok := FetchRouterUsage(context.Background(), "https://api.openai.com/v1", "t"); ok {
		t.Error("a public host must never be contacted")
	}
}
