// Package main provides the entry point for the Deneb gateway server.
//
// This will eventually replace the TypeScript gateway (src/gateway/server.impl.ts).
// Currently it serves as the scaffolding for the Go gateway.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/choiceoh/deneb/gateway-go/internal/server"
)

func main() {
	port := flag.Int("port", 18789, "Gateway server port")
	bind := flag.String("bind", "loopback", "Bind address: 'loopback' or 'all'")
	flag.Parse()

	bindAddr := "127.0.0.1"
	if *bind == "all" {
		bindAddr = "0.0.0.0"
	}

	addr := fmt.Sprintf("%s:%d", bindAddr, *port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	srv := server.New(addr)
	log.Printf("deneb gateway starting on %s", addr)

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("gateway error: %v", err)
	}
}
