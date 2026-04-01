// Binary deneb-mcp is an MCP server that bridges Claude Desktop to the Deneb
// gateway. It communicates with Claude Desktop via stdio (JSON-RPC 2.0) and
// forwards tool calls to the gateway's HTTP RPC endpoint.
//
// Usage:
//
//	deneb-mcp [flags]
//
// Flags:
//
//	--gateway-url  Gateway URL (default: http://127.0.0.1:18789)
//	--verbose      Enable debug logging to stderr
//	--version      Print version and exit
//
// Environment:
//
//	DENEB_TOKEN          Bearer token for gateway auth
//	DENEB_GATEWAY_TOKEN  Alternative token env var
//	DENEB_GATEWAY_URL    Gateway URL (overridden by --gateway-url flag)
//	DENEB_MCP_TIMEOUT    RPC timeout duration (default: 30s)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/mcp"
)

// Version is injected at build time via ldflags.
var Version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		gatewayURL string
		verbose    bool
		showVer    bool
	)

	flag.StringVar(&gatewayURL, "gateway-url", "", "Gateway URL (default: http://127.0.0.1:18789)")
	flag.BoolVar(&verbose, "verbose", false, "Enable debug logging to stderr")
	flag.BoolVar(&showVer, "version", false, "Print version and exit")
	flag.Parse()

	if showVer {
		fmt.Fprintf(os.Stdout, "deneb-mcp %s\n", Version)
		return 0
	}

	// Logger writes to stderr (stdout is reserved for MCP protocol).
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	// Resolve gateway URL.
	if gatewayURL == "" {
		gatewayURL = os.Getenv("DENEB_GATEWAY_URL")
	}
	if gatewayURL == "" {
		gatewayURL = "http://127.0.0.1:18789"
	}

	// Resolve timeout.
	timeout := 30 * time.Second
	if t := os.Getenv("DENEB_MCP_TIMEOUT"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			timeout = d
		}
	}

	// Resolve auth token.
	token := mcp.ResolveToken()

	// Create bridge to gateway.
	bridge := mcp.NewBridge(mcp.BridgeConfig{
		GatewayURL: gatewayURL,
		Token:      token,
		Timeout:    timeout,
	})

	// Verify gateway is reachable.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bridge.HealthCheck(ctx); err != nil {
		logger.Warn("gateway health check failed (continuing anyway)", "err", err)
	} else {
		logger.Info("gateway connected", "url", gatewayURL)
	}

	// Create transport (stdin/stdout).
	transport := mcp.NewTransport(os.Stdin, os.Stdout, logger)

	// Create and run server.
	server := mcp.NewServer(transport, mcp.ServerConfig{
		Bridge:  bridge,
		Logger:  logger,
		Version: Version,
	})

	// Handle signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		logger.Info("received interrupt, shutting down")
		cancel()
	}()

	logger.Info("deneb-mcp server starting", "version", Version, "gateway", gatewayURL)

	if err := server.Run(ctx); err != nil {
		logger.Error("server error", "err", err)
		return 1
	}

	return 0
}
