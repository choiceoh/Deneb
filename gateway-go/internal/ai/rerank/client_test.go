package rerank

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestClientRestoresResultIndexOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var request rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "model-a" || len(request.Documents) != 2 {
			t.Fatalf("request = %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "model-a")
	scores, err := client.Rerank(context.Background(), "query", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[0] != 0.1 || scores[1] != 0.9 {
		t.Fatalf("scores = %v", scores)
	}
}

func TestNewFromEnvRequiresExplicitURL(t *testing.T) {
	t.Setenv("DENEB_RERANK_URL", "")
	if client := NewFromEnv(); client != nil {
		t.Fatalf("client = %#v, want nil", client)
	}
}

func TestClientRejectsConcurrentOptionalRerankWithoutQueueing(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = w.Write([]byte(`{"scores":[0.9,0.1]}`))
	}))
	defer server.Close()

	client := New(server.URL, "model-a")
	first := make(chan error, 1)
	go func() {
		_, err := client.Rerank(context.Background(), "first", []string{"a", "b"})
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first rerank did not reach server")
	}

	before := time.Now()
	_, err := client.Rerank(context.Background(), "second", []string{"c", "d"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent error = %v, want ErrBusy", err)
	}
	if elapsed := time.Since(before); elapsed > 100*time.Millisecond {
		t.Fatalf("busy fail-open queued for %s", elapsed)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first rerank: %v", err)
	}
	stats := client.Stats()
	if stats.Requests != 2 || stats.Successes != 1 || stats.Failures != 0 || stats.Busy != 1 || stats.InFlight != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}
