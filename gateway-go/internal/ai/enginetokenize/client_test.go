package enginetokenize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tokenizeServer(t *testing.T, count int, normalized bool) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/tokenize" {
			t.Errorf("path = %q, want /tokenize", r.URL.Path)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		// The count must be about the caller's string, not about a prompt the
		// engine would frame with BOS.
		if req["add_special_tokens"] != false {
			t.Errorf("add_special_tokens = %v, want false", req["add_special_tokens"])
		}
		out := map[string]any{"count": count, "max_model_len": 1048576}
		if normalized {
			out["normalized"] = "NFC"
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestNewDerivesTokenizeFromAnyEngineURL(t *testing.T) {
	for _, in := range []string{
		"http://127.0.0.1:8000/metrics",
		"http://127.0.0.1:8000/v1",
		"http://127.0.0.1:8000",
		"http://100.125.220.117:8000/metrics", // CGNAT: the fleet's own range
		"http://10.10.10.2:8000/v1",
	} {
		c := New(in)
		if c == nil {
			t.Fatalf("New(%q) = nil, want a client", in)
		}
		if !strings.HasSuffix(c.URL(), "/tokenize") || strings.Contains(c.URL(), "/v1/tokenize") {
			t.Errorf("New(%q).URL() = %q", in, c.URL())
		}
	}
}

// The guard is the same one the other engine probes use: a public host, or a
// URL carrying credentials, is never contacted.
func TestNewRefusesAnythingButAnOwnedHost(t *testing.T) {
	for _, in := range []string{
		"", "not a url", "ftp://127.0.0.1/x",
		"https://api.openai.com/v1",
		"http://8.8.8.8:8000/v1",
		"http://user:pass@127.0.0.1:8000/v1",
		"http://127.0.0.1:8000/v1?token=abc",
		"http://127.0.0.1:8000/v1#frag",
	} {
		if c := New(in); c != nil {
			t.Errorf("New(%q) = %q, want nil", in, c.URL())
		}
	}
}

func TestCountReturnsEngineTruth(t *testing.T) {
	srv, _ := tokenizeServer(t, 42, false)
	res, ok := New(srv.URL).Count(context.Background(), "안녕하세요")
	if !ok || res.Count != 42 || res.MaxModelLen != 1048576 {
		t.Fatalf("Count = %+v ok=%v", res, ok)
	}
}

func TestNilClientAndOversizedInputMiss(t *testing.T) {
	var nilClient *Client
	if _, ok := nilClient.Count(context.Background(), "x"); ok {
		t.Error("a nil client must miss")
	}
	srv, calls := tokenizeServer(t, 1, false)
	if _, ok := New(srv.URL).Count(context.Background(), strings.Repeat("a", MaxTextBytes+1)); ok {
		t.Error("an oversized input must miss")
	}
	if n := *calls; n != 0 {
		t.Errorf("oversized input still reached the engine: %d calls", n)
	}
}

// A lookup never blocks a turn: the first call misses and schedules the fill,
// later calls are exact and cost nothing.
func TestCounterFillsInBackgroundThenServesFromCache(t *testing.T) {
	srv, calls := tokenizeServer(t, 1234, false)
	c := NewCounter(srv.URL, nil)

	if _, ok := c.Exact("the prompt head"); ok {
		t.Fatal("first lookup must miss")
	}
	var got int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n, ok := c.Exact("the prompt head"); ok {
			got = n
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got != 1234 {
		t.Fatalf("cached count = %d, want 1234", got)
	}
	for i := 0; i < 5; i++ {
		if n, ok := c.Exact("the prompt head"); !ok || n != 1234 {
			t.Fatalf("repeat lookup = %d ok=%v", n, ok)
		}
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("engine was asked %d times, want exactly 1", n)
	}
}

// A normalized answer counted a different string than the caller holds, so it
// is not an answer about those bytes and must not be cached as one.
func TestCounterDropsNormalizedAnswers(t *testing.T) {
	srv, _ := tokenizeServer(t, 99, true)
	c := NewCounter(srv.URL, nil)
	c.Exact("text")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := c.Exact("text"); ok {
			t.Fatal("a normalized answer was cached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNilCounterMisses(t *testing.T) {
	if c := NewCounter("https://api.openai.com/v1", nil); c != nil {
		t.Fatal("a public URL must not yield a counter")
	}
	var c *Counter
	if _, ok := c.Exact("x"); ok {
		t.Error("a nil counter must miss")
	}
}
