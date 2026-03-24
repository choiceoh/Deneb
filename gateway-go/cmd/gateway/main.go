// Package main provides the entry point for the Deneb gateway server.
//
// This replaces the TypeScript gateway (src/gateway/server.impl.ts)
// with a Go implementation supporting HTTP health, WebSocket + RPC dispatch,
// Plugin Host bridge, daemon management, and subsystem orchestration.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/bridge"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
)

func main() {
	configPath := flag.String("config", "", "Path to deneb.json config file")
	port := flag.Int("port", 0, "Gateway server port (overrides config)")
	bind := flag.String("bind", "", "Bind address: 'loopback', 'lan', 'all', 'custom', 'tailnet' (overrides config)")
	bridgeSocket := flag.String("bridge", "", "Path to plugin host unix socket")
	version := flag.String("version", "0.1.0-go", "Server version string")
	pidFile := flag.String("pid-file", "", "Path to PID file for daemon mode")
	daemonMode := flag.Bool("daemon", false, "Run as daemon (write PID file, check for existing)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (overrides config)")
	flag.Parse()

	// Bootstrap config from ~/.deneb/deneb.json (or --config path).
	bootstrap, err := config.BootstrapGatewayConfig(config.BootstrapOptions{
		ConfigPath: *configPath,
		Persist:    true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config bootstrap failed: %v\n", err)
		os.Exit(1)
	}

	// Resolve log level: CLI flag > config > default.
	resolvedLogLevel := "info"
	if bootstrap.Config.Logging != nil && bootstrap.Config.Logging.Level != "" {
		resolvedLogLevel = bootstrap.Config.Logging.Level
	}
	if *logLevel != "" {
		resolvedLogLevel = *logLevel
	}
	level := parseLogLevel(resolvedLogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	// Resolve port: CLI flag > env > config > default.
	resolvedPort := config.ResolveGatewayPort(&bootstrap.Config)
	if *port > 0 {
		resolvedPort = *port
	}

	// Resolve runtime config (bind host, auth, validation constraints).
	rtCfg, err := config.ResolveGatewayRuntimeConfig(config.RuntimeConfigParams{
		Config: &bootstrap.Config,
		Port:   resolvedPort,
		Bind:   *bind,
		Auth:   &bootstrap.Auth,
	})
	if err != nil {
		logger.Error("runtime config resolution failed", "error", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", rtCfg.BindHost, rtCfg.Port)

	srv := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(*version),
		server.WithConfig(rtCfg),
	)

	if bootstrap.GeneratedToken != "" {
		logger.Info("gateway auth token auto-generated",
			"persisted", bootstrap.PersistedGeneratedToken,
			"configPath", bootstrap.Snapshot.Path,
		)
	}

	// Daemon mode: manage PID file and check for existing daemon.
	if *daemonMode || *pidFile != "" {
		pidPath := *pidFile
		if pidPath == "" {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				home = "/tmp"
				logger.Warn("could not determine home directory, using /tmp for pid file", "error", err)
			}
			pidPath = filepath.Join(home, ".deneb", "gateway.pid")
		}

		d := daemon.NewDaemon(pidPath, *port, *version, logger)

		// Check for existing daemon.
		if existing := d.CheckExistingDaemon(); existing != nil {
			logger.Error("another daemon is already running",
				"pid", existing.PID,
				"port", existing.Port,
				"version", existing.Version,
			)
			os.Exit(1)
		}

		srv.SetDaemon(d)

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if err := d.Start(stop); err != nil {
			logger.Error("daemon start failed", "error", err)
			os.Exit(1)
		}
		defer d.Stop()

		// Connect bridge.
		connectBridge(srv, *bridgeSocket, logger)

		logger.Info("deneb gateway starting (daemon mode)", "addr", addr, "pid", os.Getpid())

		if err := srv.Run(ctx); err != nil {
			logger.Error("gateway error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Non-daemon mode (original behavior).
	connectBridge(srv, *bridgeSocket, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("deneb gateway starting", "addr", addr)

	if err := srv.Run(ctx); err != nil {
		logger.Error("gateway error", "error", err)
		os.Exit(1)
	}
}

func connectBridge(srv *server.Server, socketPath string, logger *slog.Logger) {
	if socketPath == "" {
		return
	}
	b := bridge.NewWithSocket(socketPath, logger)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := b.ConnectWithReconnect(connectCtx); err != nil {
		logger.Warn("plugin host bridge not available, will retry", "socket", socketPath, "error", err)
	} else {
		logger.Info("plugin host bridge connected", "socket", socketPath)
	}
	connectCancel()
	srv.SetBridge(b)
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
