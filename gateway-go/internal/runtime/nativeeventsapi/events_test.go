package nativeeventsapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
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
	hub := nativepush.NewHub()
	events, unsub := hub.Subscribe(nativepush.KindMobile)
	defer unsub()

	out := &syncBuffer{}
	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(clientCtx, context.Background(), out, out, events, nil)
	}()

	hub.Publish(nativepush.Event{Title: "Deneb", Body: "morning letter"})

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
	hub := nativepush.NewHub()
	events, unsub := hub.Subscribe(nativepush.KindMobile)
	defer unsub()

	rec := httptest.NewRecorder()
	shutdownCtx, shutdown := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(context.Background(), shutdownCtx, rec, rec, events, nil)
	}()

	shutdown()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamPushEvents did not return after shutdown")
	}
}

func TestStreamPushEvents_GatewayFramesInterleave(t *testing.T) {
	events := make(chan nativepush.Event)
	frames := make(chan []byte, 1)
	out := &syncBuffer{}
	clientCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPushEvents(clientCtx, context.Background(), out, out, events, frames)
	}()

	frames <- []byte(`{"type":"event","event":"agent.event","payload":{"kind":"tool.start"}}`)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "event: gateway") {
		time.Sleep(10 * time.Millisecond)
	}
	if got := out.String(); !strings.Contains(got, "event: gateway") || !strings.Contains(got, `"kind":"tool.start"`) {
		t.Fatalf("gateway frame missing from stream: %q", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("streamPushEvents did not return after client cancel")
	}
}

// The broadcaster attach must never block fan-out: a full frame buffer drops.
func TestGatewayEventSubscriber_DropsWhenBacklogged(t *testing.T) {
	sub := &gatewayEventSubscriber{id: "c1", ch: make(chan []byte, 1)}
	if err := sub.SendEvent([]byte("a")); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := sub.SendEvent([]byte("b")); err != nil {
		t.Fatalf("backlogged send must drop, not error: %v", err)
	}
	if got := sub.dropped.Load(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if len(sub.ch) != 1 {
		t.Fatalf("buffered = %d, want the first frame only", len(sub.ch))
	}
}

// Which client kinds join the gateway event plane. The phone was excluded
// originally on the belief its daemon only understood `push` frames; it in
// fact ignores unknown event names, and the exclusion left mobile with no
// live spectate narration (3s transcript polling only).
func TestJoinsGatewayPlane(t *testing.T) {
	if !joinsGatewayPlane(nativepush.KindDesktop) {
		t.Error("desktop (andromeda) must join the plane")
	}
	if !joinsGatewayPlane(nativepush.KindMobile) {
		t.Error("mobile must join the plane — it drives the phone's live spectate chips")
	}
	// An unidentified client made no claim about understanding the plane.
	if joinsGatewayPlane(nativepush.KindUnknown) {
		t.Error("unknown kind must not be sent frames it never asked for")
	}
}

// The header a surface sends is what decides the attach, so the mapping and
// the gate have to agree — a rename on either side silently un-wires spectate.
func TestClientKindHeaderReachesTheGatewayPlane(t *testing.T) {
	for _, header := range []string{"mobile", "desktop"} {
		if !joinsGatewayPlane(nativepush.ClientKindFromHeader(header)) {
			t.Errorf("X-Deneb-Client-Kind: %s must reach the gateway plane", header)
		}
	}
}
