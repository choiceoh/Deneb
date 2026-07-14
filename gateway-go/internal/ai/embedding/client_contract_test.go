package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newHealthyClient(baseURL string, client *http.Client) *Client {
	c := &Client{baseURL: baseURL, http: client, logger: slog.Default()}
	c.healthy.Store(true)
	return c
}

func TestNewDefaultsNormalizesURLAndAcceptsNilLogger(t *testing.T) {
	var healthCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			healthCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := New(server.URL+"///", nil)
	defer client.Shutdown()
	if client.baseURL != server.URL || client.logger == nil || client.http.Timeout != defaultTimeout {
		t.Fatalf("client = base=%q logger=%v timeout=%v", client.baseURL, client.logger, client.http.Timeout)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !client.IsHealthy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.IsHealthy() || healthCalls.Load() == 0 {
		t.Fatalf("initial health probe did not succeed: healthy=%v calls=%d", client.IsHealthy(), healthCalls.Load())
	}
	client.Shutdown()
}

func TestEmbedGuardClausesRejectWithoutHTTPCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newHealthyClient(server.URL, server.Client())
	if got, err := client.Embed(context.Background(), nil); err != nil || got != nil {
		t.Fatalf("empty Embed = %v,%v", got, err)
	}
	client.healthy.Store(false)
	if got, err := client.Embed(context.Background(), []string{"text"}); err == nil || got != nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("unhealthy Embed = %v,%v", got, err)
	}
	client.healthy.Store(true)
	tooMany := make([]string, maxTextsPerBatch+1)
	if got, err := client.Embed(context.Background(), tooMany); err == nil || got != nil || !strings.Contains(err.Error(), "exceeds max") {
		t.Fatalf("oversized Embed = %v,%v", got, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("guarded requests reached HTTP %d times", calls.Load())
	}
}

func TestEmbedRequestAndSuccessfulResponseContract(t *testing.T) {
	var seenMethod, seenPath, seenType string
	var seenTexts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod, seenPath, seenType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		var request embedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("request decode: %v", err)
		}
		seenTexts = request.Texts
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embeddings":[[1,2,3],[4,5,6]],"dimensions":3,"count":2}`)
	}))
	defer server.Close()
	client := newHealthyClient(server.URL, server.Client())
	vectors, err := client.Embed(context.Background(), []string{"alpha", "베타"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if seenMethod != http.MethodPost || seenPath != "/embed" || seenType != "application/json" {
		t.Fatalf("request = %s %s type=%q", seenMethod, seenPath, seenType)
	}
	if strings.Join(seenTexts, "|") != "alpha|베타" || len(vectors) != 2 || len(vectors[0]) != 3 || vectors[1][2] != 6 {
		t.Fatalf("request texts=%#v vectors=%#v", seenTexts, vectors)
	}
}

func TestEmbedRejectsMalformedResponseMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http error", status: http.StatusServiceUnavailable, body: "temporarily down", want: "HTTP 503"},
		{name: "malformed JSON", status: http.StatusOK, body: "{", want: "decode"},
		{name: "too few vectors", status: http.StatusOK, body: `{"embeddings":[]}`, want: "expected 1 embeddings"},
		{name: "too many vectors", status: http.StatusOK, body: `{"embeddings":[[1],[2]]}`, want: "expected 1 embeddings"},
		{name: "empty vector", status: http.StatusOK, body: `{"embeddings":[[]],"count":1}`, want: "embedding 0 is empty"},
		{name: "dimension mismatch", status: http.StatusOK, body: `{"embeddings":[[1,2]],"dimensions":3,"count":1}`, want: "has 2 dimensions"},
		{name: "count mismatch", status: http.StatusOK, body: `{"embeddings":[[1,2]],"dimensions":2,"count":2}`, want: "response count 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := newHealthyClient(server.URL, server.Client())
			vectors, err := client.Embed(context.Background(), []string{"alpha"})
			if err == nil || vectors != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Embed = %#v,%v; want %q", vectors, err, tc.want)
			}
			if !client.IsHealthy() {
				t.Fatal("application response error marked reachable server unhealthy")
			}
		})
	}
}

func TestEmbedHTTPErrorBodyIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("x", 10_000))
	}))
	defer server.Close()
	client := newHealthyClient(server.URL, server.Client())
	_, err := client.Embed(context.Background(), []string{"alpha"})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if got := len(err.Error()); got > 600 {
		t.Fatalf("error body was not bounded: %d chars", got)
	}
}

type errorRoundTripper struct{ err error }

func (e errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

func TestEmbedCancellationAndDeadlineKeepHealth(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		client := newHealthyClient("http://embedding.invalid", &http.Client{Transport: errorRoundTripper{err: cause}})
		_, err := client.Embed(context.Background(), []string{"alpha"})
		if err == nil || !errors.Is(err, cause) {
			t.Fatalf("Embed cause %v = %v", cause, err)
		}
		if !client.IsHealthy() {
			t.Fatalf("caller cause %v marked server unhealthy", cause)
		}
	}
	transportErr := errors.New("connection reset")
	client := newHealthyClient("http://embedding.invalid", &http.Client{Transport: errorRoundTripper{err: transportErr}})
	_, err := client.Embed(context.Background(), []string{"alpha"})
	if err == nil || !errors.Is(err, transportErr) || client.IsHealthy() {
		t.Fatalf("transport failure = %v healthy=%v", err, client.IsHealthy())
	}
}

func TestProbeUpdatesHealthOnFailureRecoveryAndCancel(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			t.Errorf("probe request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{baseURL: server.URL, http: server.Client(), logger: slog.Default(), ctx: ctx, cancel: cancel}
	client.healthy.Store(true)
	client.probe()
	if client.IsHealthy() {
		t.Fatal("failing probe left client healthy")
	}
	status.Store(http.StatusOK)
	client.probe()
	if !client.IsHealthy() {
		t.Fatal("successful probe did not restore health")
	}
	cancel()
	client.probe()
	if client.IsHealthy() {
		t.Fatal("canceled client probe left health true")
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{ctx: ctx, cancel: cancel}
	client.Shutdown()
	client.Shutdown()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Shutdown did not cancel context")
	}
}
