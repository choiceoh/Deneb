package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBook_TransitionsAndRecovers(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	book := newCircuitBook()
	book.now = func() time.Time { return now }

	view, opened := book.recordFailure("glm", false, 0)
	if opened || view.State != circuitDegraded || view.Failures != 1 {
		t.Fatalf("first failure = %+v opened=%v, want degraded/false", view, opened)
	}
	view, opened = book.recordFailure("glm", false, 0)
	if !opened || view.State != circuitOpen || view.RetryAfterMS != circuitBaseCooldown.Milliseconds() {
		t.Fatalf("second failure = %+v opened=%v, want open/%dms", view, opened, circuitBaseCooldown.Milliseconds())
	}

	now = now.Add(circuitBaseCooldown)
	if view = book.view("glm"); view.State != circuitHalfOpen {
		t.Fatalf("expired circuit = %+v, want half_open", view)
	}
	if !book.recordSuccess("glm") {
		t.Fatal("recovery should report prior failure state")
	}
	if view = book.view("glm"); view.State != circuitClosed || view.Failures != 0 {
		t.Fatalf("recovered circuit = %+v, want closed", view)
	}
}

func TestCircuitBook_OrdersOpenCandidatesLastButKeepsFailOpenPlan(t *testing.T) {
	book := newCircuitBook()
	book.recordFailure("primary", true, 0)
	candidates := []modelEntry{{Name: "primary"}, {Name: "backup"}}
	ordered := book.order(candidates)
	if got := []string{ordered[0].Name, ordered[1].Name}; got[0] != "backup" || got[1] != "primary" {
		t.Fatalf("ordered names = %v, want [backup primary]", got)
	}

	book.recordFailure("backup", true, 0)
	ordered = book.order(candidates)
	if ordered[0].Name != "primary" || ordered[1].Name != "backup" {
		t.Fatalf("all-open order must stay a usable fail-open plan, got %v", []string{ordered[0].Name, ordered[1].Name})
	}
}

func TestFallback_RateLimitedPrimaryOpensCircuitAndSkipsNextRequest(t *testing.T) {
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryHits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"from":"backup"}`)
	}))
	defer backup.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "primary", URL: primary.URL + "/v1", UpstreamModel: "primary", Fallback: "backup"},
		{Name: "backup", URL: backup.URL + "/v1", UpstreamModel: "backup"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
			strings.NewReader(`{"model":"primary"}`))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"from":"backup"`) {
			t.Fatalf("request %d = status %d body %q, want backup", i+1, resp.StatusCode, body)
		}
	}
	if got := primaryHits.Load(); got != 1 {
		t.Fatalf("rate-limited primary hits = %d, want 1 (second request should skip cooldown)", got)
	}
	view := rt.circuits.view("primary")
	if view.State != circuitOpen || view.RetryAfterMS < 59_000 {
		t.Fatalf("primary circuit = %+v, want open with Retry-After near 60s", view)
	}
}

func TestStatus_ExposesModelCircuitState(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{{
		Name: "glm", URL: "https://api.example.com/v1", UpstreamModel: "glm",
	}}})
	rt.circuits.recordFailure("glm", true, 0)
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out statusOut
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Models) != 1 {
		t.Fatalf("status models = %d, want 1", len(out.Models))
	}
	model := out.Models[0]
	if model.CircuitState != circuitOpen || model.CircuitFailures != circuitFailureThreshold || model.RetryAfterMS <= 0 {
		t.Fatalf("status circuit = %+v, want open with failures and retry delay", model)
	}
}

func TestRetryAfterHint_ParsesSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	if got := retryAfterHint("30", now); got != 30*time.Second {
		t.Fatalf("seconds hint = %s, want 30s", got)
	}
	if got := retryAfterHint(now.Add(45*time.Second).Format(http.TimeFormat), now); got != 45*time.Second {
		t.Fatalf("date hint = %s, want 45s", got)
	}
	if got := retryAfterHint("garbage", now); got != 0 {
		t.Fatalf("invalid hint = %s, want 0", got)
	}
}
