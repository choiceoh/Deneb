package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/coder/websocket"
)

func TestSmokeHealthEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.New("127.0.0.1:0")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/health", addr.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestSmokeWebSocketRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.New("127.0.0.1:0")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	wsURL := fmt.Sprintf("ws://%s/ws", addr.String())
	conn, wsResp, err := websocket.Dial(ctx, wsURL, nil)
	if wsResp != nil && wsResp.Body != nil {
		wsResp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Read connect.challenge event first.
	challengeCtx, challengeCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _, err = conn.Read(challengeCtx)
	challengeCancel()
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}

	// Handshake.
	connectReq, _ := protocol.NewRequestFrame("smoke-hs", "connect", protocol.ConnectParams{
		MinProtocol: 1, MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "smoke-test", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	_, helloData, err := conn.Read(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}

	var helloResp protocol.ResponseFrame
	if err := json.Unmarshal(helloData, &helloResp); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if !helloResp.OK {
		t.Fatalf("handshake failed: %+v", helloResp.Error)
	}

	// Health RPC.
	healthReq, _ := protocol.NewRequestFrame("smoke-rpc", "health", nil)
	data, _ = json.Marshal(healthReq)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write health: %v", err)
	}

	readCtx, readCancel = context.WithTimeout(ctx, 2*time.Second)
	_, rpcData, err := conn.Read(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("read rpc: %v", err)
	}

	var rpcResp protocol.ResponseFrame
	if err := json.Unmarshal(rpcData, &rpcResp); err != nil {
		t.Fatalf("unmarshal rpc: %v", err)
	}
	if !rpcResp.OK {
		t.Errorf("health RPC failed: %+v", rpcResp.Error)
	}

	// Unknown method -> NOT_FOUND.
	unknownReq, _ := protocol.NewRequestFrame("smoke-unknown", "nonexistent", nil)
	data, _ = json.Marshal(unknownReq)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	readCtx, readCancel = context.WithTimeout(ctx, 2*time.Second)
	_, errData, err := conn.Read(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var errResp protocol.ResponseFrame
	if err := json.Unmarshal(errData, &errResp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errResp.OK {
		t.Error("expected error for unknown method")
	}
	if errResp.Error == nil || errResp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND, got: %+v", errResp.Error)
	}
}
