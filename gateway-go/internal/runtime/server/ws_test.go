package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/ws"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestWebSocketHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	wsURL := fmt.Sprintf("ws://%s/ws", addr.String())
	conn, _, err := ws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(ws.StatusNormalClosure, "")

	// Read connect.challenge event first.
	challengeCtx, challengeCancel := context.WithTimeout(ctx, 2*time.Second)
	defer challengeCancel()
	_, challengeData, err := conn.Read(challengeCtx)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	var challengeEvent map[string]any
	if err := json.Unmarshal(challengeData, &challengeEvent); err != nil {
		t.Fatalf("unmarshal challenge: %v", err)
	}
	if challengeEvent["event"] != "connect.challenge" {
		t.Fatalf("expected connect.challenge event, got %v", challengeEvent["event"])
	}

	// Send connect request.
	connectReq, _ := protocol.NewRequestFrame("hs-1", "connect", protocol.ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "test-client", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	if err := conn.Write(ctx, ws.MessageText, data); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	// Read HelloOk.
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}

	var resp protocol.ResponseFrame
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}

	var hello protocol.HelloOk
	if err := json.Unmarshal(resp.Payload, &hello); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
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

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	conn := connectWS(ctx, t, addr.String())
	defer conn.Close(ws.StatusNormalClosure, "")

	// Send health RPC.
	healthReq, _ := protocol.NewRequestFrame("rpc-1", "health", nil)
	data, _ := json.Marshal(healthReq)
	if err := conn.Write(ctx, ws.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp protocol.ResponseFrame
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Errorf("health RPC failed: %+v", resp.Error)
	}
}

func TestWebSocketRPCUnknownMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	conn := connectWS(ctx, t, addr.String())
	defer conn.Close(ws.StatusNormalClosure, "")

	req, _ := protocol.NewRequestFrame("rpc-2", "nonexistent.method", nil)
	data, _ := json.Marshal(req)
	if err := conn.Write(ctx, ws.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp protocol.ResponseFrame
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.OK {
		t.Error("expected error for unknown method")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND error, got: %+v", resp.Error)
	}
}

// connectWS performs the WebSocket handshake and returns an authenticated connection.
func connectWS(ctx context.Context, t *testing.T, addr string) *ws.Conn {
	t.Helper()
	wsURL := fmt.Sprintf("ws://%s/ws", addr)
	conn, _, err := ws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Read connect.challenge event first.
	challengeCtx, challengeCancel := context.WithTimeout(ctx, 2*time.Second)
	defer challengeCancel()
	_, _, err = conn.Read(challengeCtx)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}

	connectReq, _ := protocol.NewRequestFrame("hs", "connect", protocol.ConnectParams{
		MinProtocol: 1, MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "test", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	if err := conn.Write(ctx, ws.MessageText, data); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, respData, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}

	var resp protocol.ResponseFrame
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if !resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}

	return conn
}
