package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamPushEvents_PublishReachesSSE verifies a hub publish is written to
// the SSE response as a "push" frame and that closing the client context stops
// the stream.
func TestStreamPushEvents_PublishReachesSSE(t *testing.T) {
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	rec := httptest.NewRecorder()
	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(clientCtx, context.Background(), rec, rec, events)
	}()

	hub.publish(clientPushEvent{Title: "Deneb", Body: "morning letter"})

	// Give the goroutine a moment to write the frame, then stop the stream.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "morning letter") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	out := rec.Body.String()
	if !strings.Contains(out, "event: push") {
		t.Fatalf("missing push event frame:\n%s", out)
	}
	if !strings.Contains(out, "morning letter") {
		t.Fatalf("missing pushed body:\n%s", out)
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
