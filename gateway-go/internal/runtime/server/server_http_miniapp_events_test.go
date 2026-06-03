package server

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a concurrency-safe io.Writer + http.Flusher for streaming
// tests that read the written bytes while streamPushEvents is still writing on
// another goroutine. A plain bytes.Buffer (httptest.ResponseRecorder.Body) is
// not safe for concurrent read/write and trips the race detector.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) WriteString(s string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteString(s)
}

func (b *syncBuffer) Flush() {}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestStreamPushEvents_PublishReachesSSE verifies a hub publish is written to
// the SSE response as a "push" frame and that closing the client context stops
// the stream.
func TestStreamPushEvents_PublishReachesSSE(t *testing.T) {
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	out := &syncBuffer{}
	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(clientCtx, context.Background(), out, out, events)
	}()

	hub.publish(clientPushEvent{Title: "Deneb", Body: "morning letter"})

	// Give the goroutine a moment to write the frame, then stop the stream.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "morning letter") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	body := out.String()
	if !strings.Contains(body, "event: push") {
		t.Fatalf("missing push event frame:\n%s", body)
	}
	if !strings.Contains(body, "morning letter") {
		t.Fatalf("missing pushed body:\n%s", body)
	}
}

// TestStreamPushEvents_ShutdownStops verifies the stream returns when the
// shutdown context fires even with no client activity.
func TestStreamPushEvents_ShutdownStops(t *testing.T) {
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	rec := httptest.NewRecorder()
	shutdownCtx, shutdown := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(context.Background(), shutdownCtx, rec, rec, events)
	}()

	shutdown()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamPushEvents did not return after shutdown")
	}
}
