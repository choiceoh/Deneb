// Package main provides the entry point for the Deneb gateway server.
//
// This replaces the TypeScript gateway (src/gateway/server.impl.ts)
// with a Go implementation supporting HTTP health, WebSocket + RPC dispatch,
// Plugin Host bridge, daemon management, and subsystem orchestration.
//
// Phase 3: The Go gateway is the primary process. It can spawn a Node.js
// Plugin Host child process (--plugin-host-cmd) for TypeScript plugin/extension
// execution, and connects to it via Unix domain socket bridge.
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
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func main() {
	configPath := flag.String("config", "", "Path to deneb.json config file")
	port := flag.Int("port", 0, "Gateway server port (overrides config)")
	bind := flag.String("bind", "", "Bind address: 'loopback', 'lan', 'all', 'custom', 'tailnet' (overrides config)")
	bridgeSocket := flag.String("bridge", "", "Path to existing plugin host unix socket (mutually exclusive with --plugin-host-cmd)")
	pluginHostCmd := flag.String("plugin-host-cmd", "", "Command to spawn the Node.js Plugin Host subprocess (e.g. 'node dist/plugin-host/main.js')")
	version := flag.String("version", "0.1.0-go", "Server version string")
	pidFile := flag.String("pid-file", "", "Path to PID file for daemon mode")
	daemonMode := flag.Bool("daemon", false, "Run as daemon (write PID file, check for existing)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (overrides config)")
	flag.Parse()

	// Validate mutually exclusive bridge options.
	if *bridgeSocket != "" && *pluginHostCmd != "" {
		fmt.Fprintln(os.Stderr, "--bridge and --plugin-host-cmd are mutually exclusive")
		os.Exit(1)
	}

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

	// Resolve config directory for PID file fallback.
	cfgDir := ""
	if bootstrap.Snapshot != nil && bootstrap.Snapshot.Path != "" {
		cfgDir = filepath.Dir(bootstrap.Snapshot.Path)
	}
	if cfgDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfgDir = filepath.Join(home, ".deneb")
		}
	}

	// Daemon mode: manage PID file and check for existing daemon.
	if *daemonMode || *pidFile != "" {
		pidPath := *pidFile
		if pidPath == "" {
			if cfgDir != "" {
				pidPath = filepath.Join(cfgDir, "gateway.pid")
			} else {
				pidPath = "/tmp/deneb-gateway.pid"
			}
		}

		d := daemon.NewDaemon(pidPath, resolvedPort, *version, logger)

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

		// Connect bridge (spawn or direct socket).
		spawnResult, bridgeErr := setupBridge(ctx, srv, *bridgeSocket, *pluginHostCmd, logger)
		if bridgeErr != nil {
			logger.Error("bridge setup failed", "error", bridgeErr)
			os.Exit(1)
		}
		if spawnResult != nil {
			defer spawnResult.Shutdown()
			// Prewarm primary model before channels start accepting messages.
			// Runs concurrently; channels wait 5s anyway, giving prewarm time to complete.
			go provider.PrewarmModel(ctx, spawnResult.Host, logger)
			// Start channel plugins in the Plugin Host after the bridge is ready.
			go startPluginHostChannels(ctx, spawnResult.Host, logger, config.ConfiguredChannelIDs(bootstrap.Snapshot))
		}

		logger.Info("deneb gateway starting (daemon mode)", "addr", addr, "pid", os.Getpid())

		if err := srv.Run(ctx); err != nil {
			logger.Error("gateway error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Non-daemon mode.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Connect bridge (spawn or direct socket).
	spawnResult, bridgeErr := setupBridge(ctx, srv, *bridgeSocket, *pluginHostCmd, logger)
	if bridgeErr != nil {
		logger.Error("bridge setup failed", "error", bridgeErr)
		os.Exit(1)
	}
	if spawnResult != nil {
		defer spawnResult.Shutdown()
		// Prewarm primary model before channels start accepting messages.
		// Runs concurrently; channels wait 5s anyway, giving prewarm time to complete.
		go provider.PrewarmModel(ctx, spawnResult.Host, logger)
		// Start channel plugins in the Plugin Host after the bridge is ready.
		go startPluginHostChannels(ctx, spawnResult.Host, logger, config.ConfiguredChannelIDs(bootstrap.Snapshot))
	}

	logger.Info("deneb gateway starting", "addr", addr)

	if err := srv.Run(ctx); err != nil {
		logger.Error("gateway error", "error", err)
		os.Exit(1)
	}
}

// setupBridge configures the Plugin Host bridge, either by spawning a child
// process (--plugin-host-cmd) or connecting to an existing socket (--bridge).
// Returns an error instead of calling os.Exit to allow proper cleanup.
func setupBridge(ctx context.Context, srv *server.Server, socketPath, pluginHostCmd string, logger *slog.Logger) (*bridge.SpawnResult, error) {
	if pluginHostCmd != "" {
		// Spawn the Plugin Host as a child process.
		result, err := bridge.SpawnPluginHost(ctx, bridge.SpawnConfig{
			Command: pluginHostCmd,
			Logger:  logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to spawn plugin host: %w", err)
		}
		srv.SetBridge(result.Host)
		return result, nil
	}

	if socketPath != "" {
		// Connect to an existing Plugin Host socket.
		b := bridge.NewWithSocket(socketPath, logger)
		connectCtx, connectCancel := context.WithTimeout(ctx, 5*time.Second)
		if err := b.ConnectWithReconnect(connectCtx); err != nil {
			logger.Warn("plugin host bridge not available, will retry", "socket", socketPath, "error", err)
		} else {
			logger.Info("plugin host bridge connected", "socket", socketPath)
		}
		connectCancel()
		srv.SetBridge(b)
	}

	return nil, nil
}

// startPluginHostChannels asks the Plugin Host to start only the configured
// channel plugins. Unconfigured plugins are skipped (lazy loading), reducing
// boot time and memory usage. Channels can still be configured later via
// `deneb channels setup` which triggers a targeted channel reload.
func startPluginHostChannels(ctx context.Context, host *bridge.PluginHost, logger *slog.Logger, configuredChannels []string) {
	// Give the Plugin Host a moment to finish gateway context initialization.
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Pass configured channel IDs so only those plugins are loaded at boot.
	// If empty, all channels start (backwards-compatible).
	var params map[string]any
	if len(configuredChannels) > 0 {
		params = map[string]any{"channels": configuredChannels}
		logger.Info("starting configured channels only", "channels", configuredChannels)
	}

	resp, err := host.Forward(startCtx, &protocol.RequestFrame{
		Type:   "req",
		ID:     "go-channel-start",
		Method: "plugin-host.channels.start-all",
		Params: params,
	})
	if err != nil {
		logger.Error("failed to start plugin host channels", "error", err)
		return
	}
	if resp != nil && !resp.OK {
		logger.Error("plugin host channel startup failed", "error", resp.Error)
		return
	}
	logger.Info("plugin host channels started")
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
