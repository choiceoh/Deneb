package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/server"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

func TestSmokeHealthEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := server.New("127.0.0.1:0")
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/health", addr.String())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestSmokeWebSocketRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := server.New("127.0.0.1:0")
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
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Handshake.
	connectReq, _ := protocol.NewRequestFrame("smoke-hs", "connect", protocol.ConnectParams{
		MinProtocol: 1, MaxProtocol: 5,
		Client: protocol.ConnectClientInfo{
			ID: "smoke-test", Version: "1.0.0", Platform: "test", Mode: "control",
		},
	})
	data, _ := json.Marshal(connectReq)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	_, helloData, _ := conn.Read(readCtx)
	readCancel()

	var helloResp protocol.ResponseFrame
	json.Unmarshal(helloData, &helloResp)
	if !helloResp.OK {
		t.Fatalf("handshake failed: %+v", helloResp.Error)
	}

	// Health RPC.
	healthReq, _ := protocol.NewRequestFrame("smoke-rpc", "health", nil)
	data, _ = json.Marshal(healthReq)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel = context.WithTimeout(ctx, 2*time.Second)
	_, rpcData, _ := conn.Read(readCtx)
	readCancel()

	var rpcResp protocol.ResponseFrame
	json.Unmarshal(rpcData, &rpcResp)
	if !rpcResp.OK {
		t.Errorf("health RPC failed: %+v", rpcResp.Error)
	}

	// Unknown method -> NOT_FOUND.
	unknownReq, _ := protocol.NewRequestFrame("smoke-unknown", "nonexistent", nil)
	data, _ = json.Marshal(unknownReq)
	conn.Write(ctx, websocket.MessageText, data)

	readCtx, readCancel = context.WithTimeout(ctx, 2*time.Second)
	_, errData, _ := conn.Read(readCtx)
	readCancel()

	var errResp protocol.ResponseFrame
	json.Unmarshal(errData, &errResp)
	if errResp.OK {
		t.Error("expected error for unknown method")
	}
	if errResp.Error == nil || errResp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND, got: %+v", errResp.Error)
	}
}
