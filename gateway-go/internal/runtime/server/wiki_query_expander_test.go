package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// The expander's own timeout must actually bound the call. The shared client
// raises short parent deadlines to its 5-minute minimum, which production
// showed as a recall query wedged at durMs=300001 on a backfill nobody awaited.
func TestWikiQueryExpanderHonorsItsOwnDeadline(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(blocked)

	expander := makeWikiQueryExpander(llm.NewClient(srv.URL, ""), "test-model", nil, nil)

	done := make(chan []string, 1)
	started := time.Now()
	go func() { done <- expander(context.Background(), "태안 케이블 납품 일정") }()

	select {
	case terms := <-done:
		if len(terms) != 0 {
			t.Fatalf("a timed-out expansion returned terms: %v", terms)
		}
		if elapsed := time.Since(started); elapsed > wikiExpanderTimeout+3*time.Second {
			t.Fatalf("expansion took %s — its %s budget was overridden", elapsed, wikiExpanderTimeout)
		}
	case <-time.After(wikiExpanderTimeout + 5*time.Second):
		t.Fatalf("expansion outlived its %s budget", wikiExpanderTimeout)
	}
}

// With the tiny model's engine down, expansion moves on to the tiny role's
// fallback instead of returning nothing.
func TestWikiQueryExpanderFallsBackInOrder(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
	}))
	defer dead.Close()
	var sawReasoningOff bool
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if reasoning, ok := body["reasoning"].(map[string]any); ok && reasoning["enabled"] == false {
			sawReasoningOff = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"해저케이블\n계통연계"},"finish_reason":"stop"}]}`))
	}))
	defer alive.Close()

	expander := makeWikiQueryExpander(llm.NewClient(dead.URL, ""), "tiny", nil, nil,
		wikiExpanderTarget{client: llm.NewClient(alive.URL, ""), model: "free", extraBody: llm.ThinkingOffFields("", true)})
	terms := expander(context.Background(), "완도 케이블 인허가")
	if strings.Join(terms, ",") != "해저케이블,계통연계" {
		t.Fatalf("terms = %v, want the fallback's answer", terms)
	}
	if !sawReasoningOff {
		t.Fatal("the fallback request lost its own shaping (reasoning.enabled=false)")
	}
}
