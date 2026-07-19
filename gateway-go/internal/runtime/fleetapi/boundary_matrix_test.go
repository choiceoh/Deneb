package fleetapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/sparkfleet"
)

func fleetTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFleetPathAllowedBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{
			name:   "allow GET /api/state",
			method: "GET",
			path:   "/api/state",
			want:   true,
		},
		{
			name:   "allow GET /api/services",
			method: "GET",
			path:   "/api/services",
			want:   true,
		},
		{
			name:   "allow GET /api/config",
			method: "GET",
			path:   "/api/config",
			want:   true,
		},
		{
			name:   "allow GET /api/recipes",
			method: "GET",
			path:   "/api/recipes",
			want:   true,
		},
		{
			name:   "allow GET /api/jobs",
			method: "GET",
			path:   "/api/jobs",
			want:   true,
		},
		{
			name:   "allow GET /api/evals",
			method: "GET",
			path:   "/api/evals",
			want:   true,
		},
		{
			name:   "allow GET /api/logs",
			method: "GET",
			path:   "/api/logs",
			want:   true,
		},
		{
			name:   "allow GET /api/hf/search",
			method: "GET",
			path:   "/api/hf/search",
			want:   true,
		},
		{
			name:   "allow GET /api/hf/info",
			method: "GET",
			path:   "/api/hf/info",
			want:   true,
		},
		{
			name:   "allow GET /api/hf/token",
			method: "GET",
			path:   "/api/hf/token",
			want:   true,
		},
		{
			name:   "allow POST /api/recipes/action",
			method: "POST",
			path:   "/api/recipes/action",
			want:   true,
		},
		{
			name:   "allow POST /api/recipes/reload",
			method: "POST",
			path:   "/api/recipes/reload",
			want:   true,
		},
		{
			name:   "allow POST /api/control",
			method: "POST",
			path:   "/api/control",
			want:   true,
		},
		{
			name:   "allow POST /api/models/sync",
			method: "POST",
			path:   "/api/models/sync",
			want:   true,
		},
		{
			name:   "allow POST /api/models/delete",
			method: "POST",
			path:   "/api/models/delete",
			want:   true,
		},
		{
			name:   "allow POST /api/models/download",
			method: "POST",
			path:   "/api/models/download",
			want:   true,
		},
		{
			name:   "allow POST /api/images/sync",
			method: "POST",
			path:   "/api/images/sync",
			want:   true,
		},
		{
			name:   "allow POST /api/images/delete",
			method: "POST",
			path:   "/api/images/delete",
			want:   true,
		},
		{
			name:   "allow POST /api/hf/token",
			method: "POST",
			path:   "/api/hf/token",
			want:   true,
		},
		{
			name:   "allow POST /api/eval",
			method: "POST",
			path:   "/api/eval",
			want:   true,
		},
		{
			name:   "allow POST /api/assist/logs",
			method: "POST",
			path:   "/api/assist/logs",
			want:   true,
		},
		{
			name:   "allow GET job 1",
			method: "GET",
			path:   "/api/jobs/1",
			want:   true,
		},
		{
			name:   "allow POST cancel 1",
			method: "POST",
			path:   "/api/jobs/1/cancel",
			want:   true,
		},
		{
			name:   "allow GET job job-1",
			method: "GET",
			path:   "/api/jobs/job-1",
			want:   true,
		},
		{
			name:   "allow POST cancel job-1",
			method: "POST",
			path:   "/api/jobs/job-1/cancel",
			want:   true,
		},
		{
			name:   "allow GET job job_1",
			method: "GET",
			path:   "/api/jobs/job_1",
			want:   true,
		},
		{
			name:   "allow POST cancel job_1",
			method: "POST",
			path:   "/api/jobs/job_1/cancel",
			want:   true,
		},
		{
			name:   "allow GET job job.1",
			method: "GET",
			path:   "/api/jobs/job.1",
			want:   true,
		},
		{
			name:   "allow POST cancel job.1",
			method: "POST",
			path:   "/api/jobs/job.1/cancel",
			want:   true,
		},
		{
			name:   "allow GET job ABC123",
			method: "GET",
			path:   "/api/jobs/ABC123",
			want:   true,
		},
		{
			name:   "allow POST cancel ABC123",
			method: "POST",
			path:   "/api/jobs/ABC123/cancel",
			want:   true,
		},
		{
			name:   "allow GET job 00000000-0000-0000-0000-000000000000",
			method: "GET",
			path:   "/api/jobs/00000000-0000-0000-0000-000000000000",
			want:   true,
		},
		{
			name:   "allow POST cancel 00000000-0000-0000-0000-000000000000",
			method: "POST",
			path:   "/api/jobs/00000000-0000-0000-0000-000000000000/cancel",
			want:   true,
		},
		{
			name:   "allow GET job 한글작업",
			method: "GET",
			path:   "/api/jobs/한글작업",
			want:   true,
		},
		{
			name:   "allow POST cancel 한글작업",
			method: "POST",
			path:   "/api/jobs/한글작업/cancel",
			want:   true,
		},
		{
			name:   "allow GET job a:b",
			method: "GET",
			path:   "/api/jobs/a:b",
			want:   true,
		},
		{
			name:   "allow POST cancel a:b",
			method: "POST",
			path:   "/api/jobs/a:b/cancel",
			want:   true,
		},
		{
			name:   "allow GET job ~job",
			method: "GET",
			path:   "/api/jobs/~job",
			want:   true,
		},
		{
			name:   "allow POST cancel ~job",
			method: "POST",
			path:   "/api/jobs/~job/cancel",
			want:   true,
		},
		{
			name:   "allow recipe drift weekly",
			method: "GET",
			path:   "/api/recipes/weekly/drift",
			want:   true,
		},
		{
			name:   "allow recipe drift recipe-1",
			method: "GET",
			path:   "/api/recipes/recipe-1/drift",
			want:   true,
		},
		{
			name:   "allow recipe drift recipe_1",
			method: "GET",
			path:   "/api/recipes/recipe_1/drift",
			want:   true,
		},
		{
			name:   "allow recipe drift recipe.1",
			method: "GET",
			path:   "/api/recipes/recipe.1/drift",
			want:   true,
		},
		{
			name:   "allow recipe drift ABC123",
			method: "GET",
			path:   "/api/recipes/ABC123/drift",
			want:   true,
		},
		{
			name:   "allow recipe drift 한글레시피",
			method: "GET",
			path:   "/api/recipes/한글레시피/drift",
			want:   true,
		},
		{
			name:   "reject GET job segment \"\"",
			method: "GET",
			path:   "/api/jobs/",
			want:   false,
		},
		{
			name:   "reject POST job segment \"\"",
			method: "POST",
			path:   "/api/jobs//cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"\"",
			method: "GET",
			path:   "/api/recipes//drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \".\"",
			method: "GET",
			path:   "/api/jobs/.",
			want:   false,
		},
		{
			name:   "reject POST job segment \".\"",
			method: "POST",
			path:   "/api/jobs/./cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \".\"",
			method: "GET",
			path:   "/api/recipes/./drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"..\"",
			method: "GET",
			path:   "/api/jobs/..",
			want:   false,
		},
		{
			name:   "reject POST job segment \"..\"",
			method: "POST",
			path:   "/api/jobs/../cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"..\"",
			method: "GET",
			path:   "/api/recipes/../drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a/b\"",
			method: "GET",
			path:   "/api/jobs/a/b",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a/b\"",
			method: "POST",
			path:   "/api/jobs/a/b/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a/b\"",
			method: "GET",
			path:   "/api/recipes/a/b/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a\\\\b\"",
			method: "GET",
			path:   "/api/jobs/a\\b",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a\\\\b\"",
			method: "POST",
			path:   "/api/jobs/a\\b/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a\\\\b\"",
			method: "GET",
			path:   "/api/recipes/a\\b/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a%2fb\"",
			method: "GET",
			path:   "/api/jobs/a%2fb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a%2fb\"",
			method: "POST",
			path:   "/api/jobs/a%2fb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a%2fb\"",
			method: "GET",
			path:   "/api/recipes/a%2fb/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a%2Fb\"",
			method: "GET",
			path:   "/api/jobs/a%2Fb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a%2Fb\"",
			method: "POST",
			path:   "/api/jobs/a%2Fb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a%2Fb\"",
			method: "GET",
			path:   "/api/recipes/a%2Fb/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a%5cb\"",
			method: "GET",
			path:   "/api/jobs/a%5cb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a%5cb\"",
			method: "POST",
			path:   "/api/jobs/a%5cb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a%5cb\"",
			method: "GET",
			path:   "/api/recipes/a%5cb/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a?x\"",
			method: "GET",
			path:   "/api/jobs/a?x",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a?x\"",
			method: "POST",
			path:   "/api/jobs/a?x/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a?x\"",
			method: "GET",
			path:   "/api/recipes/a?x/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a#x\"",
			method: "GET",
			path:   "/api/jobs/a#x",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a#x\"",
			method: "POST",
			path:   "/api/jobs/a#x/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a#x\"",
			method: "GET",
			path:   "/api/recipes/a#x/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a b\"",
			method: "GET",
			path:   "/api/jobs/a b",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a b\"",
			method: "POST",
			path:   "/api/jobs/a b/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a b\"",
			method: "GET",
			path:   "/api/recipes/a b/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \" a\"",
			method: "GET",
			path:   "/api/jobs/ a",
			want:   false,
		},
		{
			name:   "reject POST job segment \" a\"",
			method: "POST",
			path:   "/api/jobs/ a/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \" a\"",
			method: "GET",
			path:   "/api/recipes/ a/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a \"",
			method: "GET",
			path:   "/api/jobs/a ",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a \"",
			method: "POST",
			path:   "/api/jobs/a /cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a \"",
			method: "GET",
			path:   "/api/recipes/a /drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a\\tb\"",
			method: "GET",
			path:   "/api/jobs/a\tb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a\\tb\"",
			method: "POST",
			path:   "/api/jobs/a\tb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a\\tb\"",
			method: "GET",
			path:   "/api/recipes/a\tb/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a\\nb\"",
			method: "GET",
			path:   "/api/jobs/a\nb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a\\nb\"",
			method: "POST",
			path:   "/api/jobs/a\nb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a\\nb\"",
			method: "GET",
			path:   "/api/recipes/a\nb/drift",
			want:   false,
		},
		{
			name:   "reject GET job segment \"a\\r\\nb\"",
			method: "GET",
			path:   "/api/jobs/a\r\nb",
			want:   false,
		},
		{
			name:   "reject POST job segment \"a\\r\\nb\"",
			method: "POST",
			path:   "/api/jobs/a\r\nb/cancel",
			want:   false,
		},
		{
			name:   "reject recipe segment \"a\\r\\nb\"",
			method: "GET",
			path:   "/api/recipes/a\r\nb/drift",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/\"",
			method: "GET",
			path:   "/",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api\"",
			method: "GET",
			path:   "/api",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/\"",
			method: "GET",
			path:   "/api/",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/jobs/one/two\"",
			method: "GET",
			path:   "/api/jobs/one/two",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/jobs/one/cancel\"",
			method: "GET",
			path:   "/api/jobs/one/cancel",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/recipes/save\"",
			method: "GET",
			path:   "/api/recipes/save",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/recipes/delete\"",
			method: "GET",
			path:   "/api/recipes/delete",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/recipes/name/raw\"",
			method: "GET",
			path:   "/api/recipes/name/raw",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/recipes/name\"",
			method: "GET",
			path:   "/api/recipes/name",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/recipes/name/drift/extra\"",
			method: "GET",
			path:   "/api/recipes/name/drift/extra",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/models/delete\"",
			method: "GET",
			path:   "/api/models/delete",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/state\"",
			method: "POST",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/jobs\"",
			method: "POST",
			path:   "/api/jobs",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/jobs/id\"",
			method: "POST",
			path:   "/api/jobs/id",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/jobs/id/cancel/extra\"",
			method: "POST",
			path:   "/api/jobs/id/cancel/extra",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/recipes/name/drift\"",
			method: "POST",
			path:   "/api/recipes/name/drift",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/recipes/save\"",
			method: "POST",
			path:   "/api/recipes/save",
			want:   false,
		},
		{
			name:   "reject \"POST\" \"/api/recipes/delete\"",
			method: "POST",
			path:   "/api/recipes/delete",
			want:   false,
		},
		{
			name:   "reject \"PUT\" \"/api/state\"",
			method: "PUT",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"PATCH\" \"/api/state\"",
			method: "PATCH",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"DELETE\" \"/api/jobs/id\"",
			method: "DELETE",
			path:   "/api/jobs/id",
			want:   false,
		},
		{
			name:   "reject \"HEAD\" \"/api/state\"",
			method: "HEAD",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"OPTIONS\" \"/api/state\"",
			method: "OPTIONS",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"get\" \"/api/state\"",
			method: "get",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"post\" \"/api/control\"",
			method: "post",
			path:   "/api/control",
			want:   false,
		},
		{
			name:   "reject \"\" \"/api/state\"",
			method: "",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \" GET\" \"/api/state\"",
			method: " GET",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"GET \" \"/api/state\"",
			method: "GET ",
			path:   "/api/state",
			want:   false,
		},
		{
			name:   "reject \"GET\" \" /api/state\"",
			method: "GET",
			path:   " /api/state",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/state \"",
			method: "GET",
			path:   "/api/state ",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"//api/state\"",
			method: "GET",
			path:   "//api/state",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/API/state\"",
			method: "GET",
			path:   "/API/state",
			want:   false,
		},
		{
			name:   "reject \"GET\" \"/api/STATE\"",
			method: "GET",
			path:   "/api/STATE",
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PathAllowed(tc.method, tc.path); got != tc.want {
				t.Fatalf("PathAllowed(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestValidFleetSegmentBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		segment string
		want    bool
	}{
		{
			name:    "ascii",
			segment: "job-1",
			want:    true,
		},
		{
			name:    "underscore",
			segment: "job_1",
			want:    true,
		},
		{
			name:    "dot within",
			segment: "job.1",
			want:    true,
		},
		{
			name:    "unicode",
			segment: "작업-1",
			want:    true,
		},
		{
			name:    "colon",
			segment: "job:1",
			want:    true,
		},
		{
			name:    "tilde",
			segment: "~job",
			want:    true,
		},
		{
			name:    "empty",
			segment: "",
			want:    false,
		},
		{
			name:    "dot",
			segment: ".",
			want:    false,
		},
		{
			name:    "dot dot",
			segment: "..",
			want:    false,
		},
		{
			name:    "slash",
			segment: "a/b",
			want:    false,
		},
		{
			name:    "backslash",
			segment: "a\\b",
			want:    false,
		},
		{
			name:    "percent",
			segment: "a%2fb",
			want:    false,
		},
		{
			name:    "question",
			segment: "a?b",
			want:    false,
		},
		{
			name:    "fragment",
			segment: "a#b",
			want:    false,
		},
		{
			name:    "space",
			segment: "a b",
			want:    false,
		},
		{
			name:    "tab",
			segment: "a\tb",
			want:    false,
		},
		{
			name:    "newline",
			segment: "a\nb",
			want:    false,
		},
		{
			name:    "carriage return",
			segment: "a\rb",
			want:    false,
		},
		{
			name:    "nonbreaking space",
			segment: "a b",
			want:    false,
		},
		{
			name:    "em space",
			segment: "a b",
			want:    false,
		},
		{
			name:    "nul",
			segment: "a\u0000b",
			want:    false,
		},
		{
			name:    "delete control",
			segment: "ab",
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := validFleetSegment(tc.segment); got != tc.want {
				t.Fatalf("validFleetSegment(%q) = %v, want %v", tc.segment, got, tc.want)
			}
		})
	}
}

func TestFleetProxyRejectsDisabledAndForbiddenRequests(t *testing.T) {
	t.Run("nil integration", func(t *testing.T) {
		rec := httptest.NewRecorder()
		New(nil, fleetTestLogger()).Proxy(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "integration is off") {
			t.Fatalf("body=%q", rec.Body.String())
		}
		if rec.Header().Get("Server") != "deneb-gateway" {
			t.Errorf("Server=%q", rec.Header().Get("Server"))
		}
	})
	t.Run("forbidden never reaches upstream", func(t *testing.T) {
		var hits atomic.Int64
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			http.Error(w, "unexpected", http.StatusTeapot)
		}))
		defer upstream.Close()
		h := New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger())
		rec := httptest.NewRecorder()
		h.Proxy(rec, httptest.NewRequest(http.MethodPost, "/api/v1/fleet/api/recipes/save", strings.NewReader(`{}`)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		if hits.Load() != 0 {
			t.Fatalf("forbidden request reached upstream %d times", hits.Load())
		}
	})
}

func TestFleetProxyForwardsRequestPreservingResponseFields(t *testing.T) {
	t.Setenv("DENEB_SPARKFLEET_TOKEN", "fleet-secret")
	type observed struct {
		method      string
		path        string
		query       string
		contentType string
		token       string
		body        string
	}
	observedCh := make(chan observed, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		observedCh <- observed{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"), token: r.Header.Get("X-Fleet-Token"), body: string(body)}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"job":"queued"}`)
	}))
	defer upstream.Close()
	h := New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/api/control?node=spark&wait=false", strings.NewReader(`{"action":"restart"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	h.Proxy(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != `{"job":"queued"}` {
		t.Errorf("body=%q", rec.Body.String())
	}
	got := <-observedCh
	if got.method != http.MethodPost {
		t.Errorf("method=%q", got.method)
	}
	if got.path != "/api/control" {
		t.Errorf("path=%q", got.path)
	}
	if got.query != "node=spark&wait=false" {
		t.Errorf("query=%q", got.query)
	}
	if got.contentType != "application/json; charset=utf-8" {
		t.Errorf("contentType=%q", got.contentType)
	}
	if got.token != "fleet-secret" {
		t.Errorf("token=%q", got.token)
	}
	if got.body != `{"action":"restart"}` {
		t.Errorf("body=%q", got.body)
	}
}

func TestFleetProxyRelaysUpstreamStatusMatrix(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{
			name:        "ok",
			status:      200,
			contentType: "application/json",
			body:        "{\"ok\":true}",
		},
		{
			name:        "created",
			status:      201,
			contentType: "application/json",
			body:        "created",
		},
		{
			name:        "accepted",
			status:      202,
			contentType: "text/plain",
			body:        "queued",
		},
		{
			name:        "no content",
			status:      204,
			contentType: "",
			body:        "",
		},
		{
			name:        "bad request",
			status:      400,
			contentType: "application/problem+json",
			body:        "bad",
		},
		{
			name:        "unauthorized",
			status:      401,
			contentType: "text/plain",
			body:        "auth",
		},
		{
			name:        "forbidden",
			status:      403,
			contentType: "text/plain",
			body:        "forbidden",
		},
		{
			name:        "not found",
			status:      404,
			contentType: "text/plain",
			body:        "missing",
		},
		{
			name:        "conflict",
			status:      409,
			contentType: "text/plain",
			body:        "conflict",
		},
		{
			name:        "too many",
			status:      429,
			contentType: "text/plain",
			body:        "slow",
		},
		{
			name:        "server error",
			status:      500,
			contentType: "text/plain",
			body:        "boom",
		},
		{
			name:        "unavailable",
			status:      503,
			contentType: "text/plain",
			body:        "down",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			rec := httptest.NewRecorder()
			New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger()).Proxy(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil))
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d", rec.Code, tc.status)
			}
			if rec.Body.String() != tc.body {
				t.Errorf("body=%q want=%q", rec.Body.String(), tc.body)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type=%q want=%q", got, tc.contentType)
			}
		})
	}
}

func TestFleetProxyUnreachableAndCanceled(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		h := New(sparkfleet.New("http://127.0.0.1:1", fleetTestLogger()), fleetTestLogger())
		rec := httptest.NewRecorder()
		h.Proxy(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unreachable") {
			t.Fatalf("body=%q", rec.Body.String())
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer upstream.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger()).Proxy(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
	})
}

func TestFleetProxyConcurrentRequests(t *testing.T) {
	const workers = 64
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		defer r.Body.Close()
		var payload map[string]int
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer upstream.Close()
	h := New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger())
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"worker":%d}`, i)
			rec := httptest.NewRecorder()
			h.Proxy(rec, httptest.NewRequest(http.MethodPost, "/api/v1/fleet/api/control", bytes.NewBufferString(body)))
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("worker %d status=%d body=%q", i, rec.Code, rec.Body.String())
				return
			}
			if !strings.Contains(rec.Body.String(), fmt.Sprintf(`"worker":%d`, i)) {
				errs <- fmt.Errorf("worker %d response=%q", i, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if hits.Load() != workers {
		t.Errorf("hits=%d want=%d", hits.Load(), workers)
	}
}

func TestFleetServeHTTPAuthenticationBoundary(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = io.WriteString(w, "ok") }))
	defer upstream.Close()
	h := New(sparkfleet.New(upstream.URL, fleetTestLogger()), fleetTestLogger())
	for _, tc := range []struct {
		name, token string
		wantStatus  int
		wantHits    int64
	}{
		{name: "missing", token: "", wantStatus: http.StatusUnauthorized, wantHits: 0},
		{name: "invalid", token: "wrong", wantStatus: http.StatusUnauthorized, wantHits: 0},
		{name: "valid", token: token, wantStatus: http.StatusOK, wantHits: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil)
			if tc.token != "" {
				req.Header.Set(clientauth.Header, tc.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
			}
			if hits.Load() != tc.wantHits {
				t.Fatalf("hits=%d want=%d", hits.Load(), tc.wantHits)
			}
		})
	}
}
