package embedding

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEmbed_ContextCancelKeepsHealth verifies a caller-side cancellation (e.g.
// recall's 1.5s preflight budget expiring) does NOT mark the server unhealthy.
// Flipping health on a transient client cancellation would disable semantic
// search, re-embedding, and SuggestRelated for the full healthCheckPeriod (30s).
// Uses a pre-cancelled context so http.Do returns context.Canceled without any
// server round-trip (the DeadlineExceeded path shares the same errors.Is guard).
func TestEmbed_ContextCancelKeepsHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[0.1]]}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, http: &http.Client{}, logger: slog.Default()}
	c.healthy.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Embed → Do returns context.Canceled, no round-trip
	if _, err := c.Embed(ctx, []string{"hi"}); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !c.IsHealthy() {
		t.Error("a client-side cancellation must not flip the server to unhealthy")
	}
}

// TestEmbed_TransportFailureFlipsHealth verifies a genuine transport failure
// (connection refused) still marks the server unhealthy, so the unreachable
// server is short-circuited until the next health probe.
func TestEmbed_TransportFailureFlipsHealth(t *testing.T) {
	// A server that is immediately closed yields a refused-connection URL.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &Client{baseURL: url, http: &http.Client{}, logger: slog.Default()}
	c.healthy.Store(true)

	if _, err := c.Embed(context.Background(), []string{"hi"}); err == nil {
		t.Fatal("expected a connection error")
	}
	if c.IsHealthy() {
		t.Error("a genuine transport failure must flip the server to unhealthy")
	}
}
