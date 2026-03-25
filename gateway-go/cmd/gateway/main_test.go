package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"syscall"
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

func TestRunWithSignals_SIGUSR1_ReturnsRestartCode(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := runWithSignals(func(ctx context.Context) error {
		// Send SIGUSR1 to self after a brief delay.
		go func() {
			time.Sleep(50 * time.Millisecond)
			syscall.Kill(os.Getpid(), syscall.SIGUSR1)
		}()
		<-ctx.Done()
		return nil
	}, logger)

	if exitCode != ExitCodeRestart {
		t.Errorf("exitCode = %d, want %d (ExitCodeRestart)", exitCode, ExitCodeRestart)
	}
}

func TestRunWithSignals_SIGTERM_ReturnsZero(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := runWithSignals(func(ctx context.Context) error {
		go func() {
			time.Sleep(50 * time.Millisecond)
			syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
		<-ctx.Done()
		return nil
	}, logger)

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
}

func TestRunWithSignals_Error_ReturnsOne(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := runWithSignals(func(_ context.Context) error {
		return errors.New("test error")
	}, logger)

	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}
