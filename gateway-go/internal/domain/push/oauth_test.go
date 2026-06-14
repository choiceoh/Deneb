package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenSource_MintsAndCaches(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if g := r.Form.Get("grant_type"); g != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", g)
		}
		if r.Form.Get("assertion") == "" {
			t.Error("missing assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":3600}`))
	}))
	defer srv.Close()

	raw, _ := testCredentials(t, srv.URL)
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ts := newTokenSource(sa, srv.Client())
	clock := time.Unix(1_700_000_000, 0)
	ts.now = func() time.Time { return clock }

	got, err := ts.accessToken(context.Background())
	if err != nil || got != "tok-1" {
		t.Fatalf("first mint: tok=%q err=%v", got, err)
	}
	// Second call within the validity window is served from cache.
	if _, err := ts.accessToken(context.Background()); err != nil {
		t.Fatalf("cached: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("token endpoint hit %d times, want 1 (cache miss)", h)
	}

	// Advancing past expiry forces a refresh.
	clock = clock.Add(2 * time.Hour)
	if _, err := ts.accessToken(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Fatalf("token endpoint hit %d times, want 2 after expiry", h)
	}
}

func TestTokenSource_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// Body would carry the token on success — assert below it never leaks.
		_, _ = w.Write([]byte(`{"error":"invalid_grant","secret":"do-not-log"}`))
	}))
	defer srv.Close()

	raw, _ := testCredentials(t, srv.URL)
	sa, _ := parseServiceAccount(raw)
	ts := newTokenSource(sa, srv.Client())

	_, err := ts.accessToken(context.Background())
	if err == nil {
		t.Fatal("expected error on non-200")
	}
	if got := err.Error(); strings.Contains(got, "do-not-log") || strings.Contains(got, "invalid_grant") {
		t.Errorf("error leaks response body: %v", err)
	}
}
