package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPluginHostNotRunning(t *testing.T) {
	h := New()
	if h.IsRunning() {
		t.Error("new plugin host should not be running")
	}
}

func TestNewRequestFrame(t *testing.T) {
	params := map[string]string{"text": "hello"}
	frame, err := protocol.NewRequestFrame("req-1", "chat.send", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Type != protocol.FrameTypeRequest {
		t.Errorf("expected type=req, got %s", frame.Type)
	}
	if frame.ID != "req-1" {
		t.Errorf("expected id=req-1, got %s", frame.ID)
	}

	var p map[string]string
	if err := json.Unmarshal(frame.Params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p["text"] != "hello" {
		t.Errorf("expected text=hello, got %s", p["text"])
	}
}

func TestFrameWriterReader(t *testing.T) {
	var buf bytes.Buffer

	writer := NewFrameWriter(&buf)
	req := &protocol.RequestFrame{Type: "req", ID: "test-1", Method: "health"}
	if err := writer.WriteRequest(req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}

	resp, _ := protocol.NewResponseOK("test-1", map[string]string{"status": "ok"})
	if err := writer.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}

	reader := NewFrameReader(&buf)
	frameType, _, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if frameType != protocol.FrameTypeRequest {
		t.Errorf("frame type = %q, want %q", frameType, protocol.FrameTypeRequest)
	}
}

func TestPluginHostForward(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// Mock server echoes requests as successful responses.
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := NewFrameReader(conn)
		writer := NewFrameWriter(conn)

		for {
			_, data, err := reader.ReadFrame()
			if err != nil {
				return
			}
			var req protocol.RequestFrame
			if err := json.Unmarshal(data, &req); err != nil {
				return
			}
			resp, _ := protocol.NewResponseOK(req.ID, map[string]string{"echo": req.Method})
			if err := writer.WriteResponse(resp); err != nil {
				return
			}
		}
	}()

	h := NewWithSocket(socketPath, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer h.Close()

	if !h.IsRunning() {
		t.Error("plugin host should be running after connect")
	}

	req := &protocol.RequestFrame{Type: "req", ID: "fwd-1", Method: "sessions.list"}
	resp, err := h.Forward(ctx, req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK response, got: %+v", resp.Error)
	}
}

func TestPluginHostForwardTimeout(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		<-time.After(10 * time.Second)
		conn.Close()
	}()

	h := NewWithSocket(socketPath, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer h.Close()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shortCancel()

	req := &protocol.RequestFrame{Type: "req", ID: "timeout-1", Method: "slow"}
	_, err = h.Forward(shortCtx, req)
	if err == nil {
		t.Error("expected timeout error")
	}
}
