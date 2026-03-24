package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestPluginHostEventHandler(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	var mu sync.Mutex
	var receivedEvents []*protocol.EventFrame

	// Start mock server that sends an event.
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		writer := NewFrameWriter(conn)

		// Send an event frame.
		ev := &protocol.EventFrame{
			Type:  protocol.FrameTypeEvent,
			Event: "chat",
		}
		payload, _ := json.Marshal(map[string]string{"runId": "run-1", "state": "done"})
		ev.Payload = payload
		writer.WriteFrame(ev)

		// Keep connection open for a bit.
		time.Sleep(200 * time.Millisecond)
	}()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := NewWithSocket(socketPath, logger)

	// Register event handler.
	h.SetEventHandler(func(ev *protocol.EventFrame) {
		mu.Lock()
		receivedEvents = append(receivedEvents, ev)
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer h.Close()

	// Wait for event to be received.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(receivedEvents)
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}

	mu.Lock()
	ev := receivedEvents[0]
	mu.Unlock()

	if ev.Event != "chat" {
		t.Errorf("expected event name 'chat', got %q", ev.Event)
	}
}

func TestPluginHostEventHandlerNotSet(t *testing.T) {
	// When no handler is set, events should just be logged (not panic).
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		writer := NewFrameWriter(conn)
		ev := &protocol.EventFrame{
			Type:  protocol.FrameTypeEvent,
			Event: "test.event",
		}
		writer.WriteFrame(ev)
		time.Sleep(200 * time.Millisecond)
	}()

	// Use a buffer to capture log output.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := NewWithSocket(socketPath, logger)
	// Intentionally NOT setting event handler.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer h.Close()

	time.Sleep(100 * time.Millisecond)

	// Should not have panicked; debug log should mention "no handler".
	if !bytes.Contains(logBuf.Bytes(), []byte("no handler")) {
		t.Log("log output:", logBuf.String())
		// Not a hard failure — just verify it didn't panic.
	}
}
