// Package main provides the entry point for the Deneb gateway server.
//
// This is the standalone Go gateway — all RPC methods are handled natively
// without a Node.js Plugin Host bridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/logging"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// ExitCodeRestart is the exit code used to signal that the gateway should be
// restarted (e.g., after receiving SIGUSR1). Wrapper scripts can check for
// this code to implement auto-restart loops. Matches EX_TEMPFAIL from sysexits.h.
const ExitCodeRestart = 75

func main() {
	configPath := flag.String("config", "", "Path to deneb.json config file")
	port := flag.Int("port", 0, "Gateway server port (overrides config)")
	bind := flag.String("bind", "", "Bind address: 'loopback', 'lan', 'all', 'custom', 'tailnet' (overrides config)")
	version := flag.String("version", "0.1.0-go", "Server version string")
	pidFile := flag.String("pid-file", "", "Path to PID file for daemon mode")
	daemonMode := flag.Bool("daemon", false, "Run as daemon (write PID file, check for existing)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (overrides config)")
	flag.Parse()

	// Load .env files before config bootstrap so env-based overrides are available.
	config.LoadDotenvFiles(slog.Default())

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

	// Resolve log format: config > default ("text").
	logFormat := "text"
	if bootstrap.Config.Logging != nil && bootstrap.Config.Logging.Format != "" {
		logFormat = bootstrap.Config.Logging.Format
	}

	var handler slog.Handler
	switch logFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	default:
		handler = logging.NewConsoleHandler(os.Stderr, &logging.ConsoleOptions{
			Level: level,
			Color: true,
		})
	}
	logger := slog.New(handler)

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

	// Detect or auto-launch embedding server (DGX Spark).
	embedResult := vega.DetectOrLaunchEmbedServer(logger)
	stopEmbed := func() {
		if embedResult.Server != nil {
			embedResult.Server.Stop()
		}
	}

	// Initialize Vega backend (SGLang-enhanced search).
	initVega(srv, logger, embedResult.Endpoint)

	// Share embedding endpoint with the memory subsystem.
	srv.SetEmbedEndpoint(embedResult.Endpoint)

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

		exitCode := runWithSignals(func(ctx context.Context) error {
			if err := d.Start(func() {}); err != nil {
				return fmt.Errorf("daemon start failed: %w", err)
			}

			go provider.PrewarmModel(ctx, logger)
			logger.Info("deneb gateway starting (daemon mode)", "addr", addr, "pid", os.Getpid())
			return srv.Run(ctx)
		}, logger)

		// Explicitly stop services before os.Exit (defers won't run).
		d.Stop()
		stopEmbed()
		os.Exit(exitCode)
	}

	// Non-daemon mode.
	exitCode := runWithSignals(func(ctx context.Context) error {
		go provider.PrewarmModel(ctx, logger)
		logger.Info("deneb gateway starting", "addr", addr)
		return srv.Run(ctx)
	}, logger)
	stopEmbed()
	os.Exit(exitCode)
}

// runWithSignals runs the given function with a context that is cancelled on
// SIGINT, SIGTERM, or SIGUSR1. Returns ExitCodeRestart (75) if SIGUSR1 was
// received, 1 on error, or 0 on clean shutdown.
func runWithSignals(run func(ctx context.Context) error, logger *slog.Logger) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	defer signal.Stop(sigCh)

	var restartRequested atomic.Bool

	go func() {
		sig := <-sigCh
		if sig == syscall.SIGUSR1 {
			logger.Info("received SIGUSR1, initiating graceful restart")
			restartRequested.Store(true)
		} else {
			logger.Info("received shutdown signal", "signal", sig)
		}
		cancel()
	}()

	if err := run(ctx); err != nil {
		logger.Error("gateway error", "error", err)
		return 1
	}

	if restartRequested.Load() {
		logger.Info("exiting for restart", "exitCode", ExitCodeRestart)
		return ExitCodeRestart
	}
	return 0
}

// initVega sets up the Vega search backend with SGLang embedding and query expansion.
func initVega(srv *server.Server, logger *slog.Logger, embed *vega.EmbedEndpoint) {
	const (
		sglangURL   = "http://127.0.0.1:30000/v1"
		sglangModel = "Qwen/Qwen3.5-35B-A3B"
	)

	if !vega.ShouldEnableVega(ffi.Available, sglangURL, logger) {
		logger.Info("vega: disabled (FFI not available)")
		return
	}

	cfg := vega.EnhancedBackendConfig{
		Logger:      logger,
		SglangURL:   sglangURL,
		SglangModel: sglangModel,
	}
	if embed != nil {
		cfg.EmbedURL = embed.URL
		cfg.EmbedModel = embed.Model
	}

	backend := vega.NewEnhancedBackend(cfg)
	srv.SetVega(backend)
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
