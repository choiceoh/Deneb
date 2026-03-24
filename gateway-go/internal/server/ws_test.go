package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

func TestWebSocketHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New("127.0.0.1:0")
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	wsURL := fmt.Sprintf("ws://%s/ws", addr.String())
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send connect request.
	connectReq, _ := protocol.NewRequestFrame("hs-1", "connect", protocol.ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "test-client", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	conn.Write(ctx, websocket.MessageText, data)

	// Read HelloOk.
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}

	var resp protocol.ResponseFrame
	json.Unmarshal(respData, &resp)
	if !resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}

	var hello protocol.HelloOk
	json.Unmarshal(resp.Payload, &hello)
	if hello.Type != "hello-ok" {
		t.Errorf("HelloOk.Type = %q, want %q", hello.Type, "hello-ok")
	}
	if hello.Protocol != protocol.ProtocolVersion {
		t.Errorf("Protocol = %d, want %d", hello.Protocol, protocol.ProtocolVersion)
	}
}

func TestWebSocketRPCHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New("127.0.0.1:0")
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	conn := connectWS(t, ctx, addr.String())
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send health RPC.
	healthReq, _ := protocol.NewRequestFrame("rpc-1", "health", nil)
	data, _ := json.Marshal(healthReq)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp protocol.ResponseFrame
	json.Unmarshal(respData, &resp)
	if !resp.OK {
		t.Errorf("health RPC failed: %+v", resp.Error)
	}
}

func TestWebSocketRPCUnknownMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New("127.0.0.1:0")
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	conn := connectWS(t, ctx, addr.String())
	defer conn.Close(websocket.StatusNormalClosure, "")

	req, _ := protocol.NewRequestFrame("rpc-2", "nonexistent.method", nil)
	data, _ := json.Marshal(req)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp protocol.ResponseFrame
	json.Unmarshal(respData, &resp)
	if resp.OK {
		t.Error("expected error for unknown method")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND error, got: %+v", resp.Error)
	}
}

// connectWS performs the WebSocket handshake and returns an authenticated connection.
func connectWS(t *testing.T, ctx context.Context, addr string) *websocket.Conn {
	t.Helper()
	wsURL := fmt.Sprintf("ws://%s/ws", addr)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	connectReq, _ := protocol.NewRequestFrame("hs", "connect", protocol.ConnectParams{
		MinProtocol: 1, MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "test", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	conn.Read(readCtx)

	return conn
}
