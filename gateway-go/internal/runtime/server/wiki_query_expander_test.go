package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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
