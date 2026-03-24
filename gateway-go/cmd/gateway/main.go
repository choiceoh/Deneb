// Package main provides the entry point for the Deneb gateway server.
//
// This replaces the TypeScript gateway (src/gateway/server.impl.ts)
// with a Go implementation supporting HTTP health, WebSocket + RPC dispatch,
// and optional Plugin Host bridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/bridge"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
)

func main() {
	port := flag.Int("port", 18789, "Gateway server port")
	bind := flag.String("bind", "loopback", "Bind address: 'loopback' or 'all'")
	bridgeSocket := flag.String("bridge", "", "Path to plugin host unix socket")
	version := flag.String("version", "0.1.0-go", "Server version string")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	bindAddr := "127.0.0.1"
	if *bind == "all" || *bind == "lan" {
		bindAddr = "0.0.0.0"
	}

	addr := fmt.Sprintf("%s:%d", bindAddr, *port)

	srv := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(*version),
	)

	// Connect to Plugin Host bridge if specified.
	if *bridgeSocket != "" {
		b := bridge.NewWithSocket(*bridgeSocket, logger)
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := b.Connect(connectCtx); err != nil {
			logger.Warn("plugin host bridge not available", "socket", *bridgeSocket, "error", err)
		} else {
			logger.Info("plugin host bridge connected", "socket", *bridgeSocket)
			srv.SetBridge(b)
		}
		connectCancel()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("deneb gateway starting", "addr", addr)

	if err := srv.Run(ctx); err != nil {
		logger.Error("gateway error", "error", err)
		os.Exit(1)
	}
}
