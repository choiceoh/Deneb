package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/logging"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// ExitCodeRestart signals that the gateway should be restarted (e.g., after
// receiving SIGUSR1). Wrapper scripts check for this code to implement
// auto-restart loops. Matches EX_TEMPFAIL from sysexits.h.
const ExitCodeRestart = 75

// RunWithSignals runs fn with a context cancelled on SIGINT, SIGTERM, or SIGUSR1.
// Returns ExitCodeRestart (75) on SIGUSR1, 1 on error, or 0 on clean shutdown.
func RunWithSignals(fn func(ctx context.Context) error, logger *slog.Logger) int {
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

	if err := fn(ctx); err != nil {
		logger.Error("gateway error", "error", err)
		return 1
	}

	if restartRequested.Load() {
		logger.Info("exiting for restart", "exitCode", ExitCodeRestart)
		return ExitCodeRestart
	}
	return 0
}

// RunDaemon runs the gateway in daemon mode: writes a PID file and checks for
// an already-running instance before starting.
func RunDaemon(flags Flags, cfg ConfigResult, svc Services, log LoggingResult) int {
	pidPath := resolvePIDPath(flags.PIDFile, cfg.CfgDir)
	d := daemon.NewDaemon(pidPath, cfg.Port, flags.Version, log.Logger)

	if existing := d.CheckExistingDaemon(); existing != nil {
		log.Logger.Error("another daemon is already running",
			"pid", existing.PID,
			"port", existing.Port,
			"version", existing.Version,
		)
		return 1
	}

	svc.Server.SetDaemon(d)

	bannerInfo := buildBannerInfo(flags.Version, cfg.Addr, svc.VegaEnabled)
	bannerInfo.PID = os.Getpid()

	svc.Server.OnListening = func(_ net.Addr) {
		logging.PrintBanner(os.Stderr, bannerInfo, log.UseColor)
	}

	exitCode := RunWithSignals(func(ctx context.Context) error {
		if err := d.Start(func() {}); err != nil {
			return fmt.Errorf("daemon start failed: %w", err)
		}
		return svc.Server.Run(ctx)
	}, log.Logger)

	d.Stop()
	return exitCode
}

// RunServer runs the gateway in non-daemon foreground mode.
func RunServer(flags Flags, cfg ConfigResult, svc Services, log LoggingResult) int {
	bannerInfo := buildBannerInfo(flags.Version, cfg.Addr, svc.VegaEnabled)
	svc.Server.OnListening = func(_ net.Addr) {
		logging.PrintBanner(os.Stderr, bannerInfo, log.UseColor)
	}
	return RunWithSignals(func(ctx context.Context) error {
		return svc.Server.Run(ctx)
	}, log.Logger)
}

func buildBannerInfo(version, addr string, vegaEnabled bool) logging.BannerInfo {
	sglangBannerURL := modelrole.DefaultSglangBaseURL
	sglangStatus := "offline"
	if vega.IsSglangReachable(sglangBannerURL) {
		sglangStatus = "online"
	}
	return logging.BannerInfo{
		Version:      version,
		Addr:         addr,
		RustFFI:      ffi.Available,
		VegaEnabled:  vegaEnabled,
		SglangStatus: sglangStatus,
	}
}

func resolvePIDPath(pidFile, cfgDir string) string {
	if pidFile != "" {
		return pidFile
	}
	if cfgDir != "" {
		return filepath.Join(cfgDir, "gateway.pid")
	}
	return "/tmp/deneb-gateway.pid"
}
