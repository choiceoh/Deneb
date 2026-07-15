package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// ExitCodeRestart signals that the gateway should be restarted (e.g., after
// receiving SIGUSR1). Wrapper scripts check for this code to implement
// auto-restart loops. Matches EX_TEMPFAIL from sysexits.h.
const ExitCodeRestart = 75

// shutdownGraceTimeout bounds how long graceful shutdown may run after a
// signal before the process is force-exited.
//
// Why this exists: a deploy sends SIGUSR1, which cancels the run context so
// the server tears down (HTTP listener closed first, then subsystems drained)
// and the process exits — systemd's Restart=always then brings up the new
// binary. But server.doShutdown drains several subsystems with their own
// (some unbounded) waits, so if any drain hangs, the process closes its HTTP
// listener and then never exits: the gateway is WEDGED — HTTP down, process
// alive, so systemd never restarts it and only a manual SIGKILL recovers it.
// (This caused a production miniapp outage.) The watchdog guarantees the
// process always terminates after a signal so a fresh listener always comes
// back. A restart may now spend up to six minutes draining a user turn before
// the bounded subsystem teardown begins, so the watchdog leaves two minutes of
// additional cleanup headroom and only fires on a genuine hang.
// Indirected as a var so tests can shorten it.
var shutdownGraceTimeout = 8 * time.Minute

// osExit is indirected so tests can assert the force-exit path without
// terminating the test binary.
var osExit = os.Exit

// RunWithSignals runs fn with a context cancelled on SIGINT, SIGTERM, or SIGUSR1.
// Returns ExitCodeRestart (75) on SIGUSR1, 1 on error, or 0 on clean shutdown.
//
// version is the running gateway's version: before honoring SIGUSR1 the
// downgrade guard interrogates the candidate binary at the executable path and
// refuses the restart when it is older (see downgrade_guard.go). Empty/"dev"
// (tests, dev builds) disables the guard.
//
// Robustness contract: once a shutdown-bound signal arrives the process is
// guaranteed to exit within shutdownGraceTimeout (force-exited if graceful
// shutdown stalls), and a repeated SIGINT/SIGTERM hard-terminates immediately —
// so a hung shutdown can never wedge the gateway with its listener closed but
// the process alive. A REFUSED SIGUSR1 does not shut anything down: the
// supervisor keeps serving and keeps listening for further signals.
func RunWithSignals(fn func(ctx context.Context) error, logger *slog.Logger, version string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Buffer > 1: the downgrade guard may hold the supervisor for up to
	// probeVersionTimeout while interrogating a candidate binary; signals
	// arriving meanwhile (systemd retries, operator INT/TERM) must not be
	// dropped by a full channel (signal.Notify discards on overflow).
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	defer signal.Stop(sigCh)

	var restartRequested atomic.Bool
	fnDone := make(chan struct{})
	supervisorDone := make(chan struct{})

	go func() {
		defer close(supervisorDone)
		for {
			select {
			case <-fnDone:
				// fn returned before any signal — nothing to supervise.
				return
			case sig := <-sigCh:
				if sig == syscall.SIGUSR1 {
					if !acceptRestart(version, logger) {
						continue // downgrade refused — keep serving, keep listening
					}
					logger.Info("received SIGUSR1, initiating graceful restart")
					restartRequested.Store(true)
				} else {
					logger.Info("received shutdown signal", "signal", sig)
				}
				cancel()

				// A repeated INT/TERM now hard-terminates: restore the default
				// disposition so an impatient operator (or systemd) can force the
				// kill if graceful shutdown stalls. SIGUSR1 stays handled.
				signal.Reset(syscall.SIGINT, syscall.SIGTERM)

				// Watchdog: force-exit if graceful shutdown overruns the grace
				// window so the process ALWAYS terminates and systemd restarts a
				// fresh listener. See shutdownGraceTimeout.
				select {
				case <-fnDone:
				case <-time.After(shutdownGraceTimeout):
					code := 0
					if restartRequested.Load() {
						code = ExitCodeRestart
					}
					logger.Error("graceful shutdown exceeded grace window; forcing exit",
						"grace", shutdownGraceTimeout, "exitCode", code)
					osExit(code)
				}
				return
			}
		}
	}()

	defer httputil.CloseIdle()

	err := fn(ctx)
	close(fnDone)
	// Join the supervisor before returning: its signal.Reset(INT, TERM) must
	// not race a subsequent RunWithSignals call's signal.Notify in the same
	// process (the signal tests hit exactly that window — a late Reset wiped
	// the next test's handler and a self-sent SIGTERM killed the binary).
	<-supervisorDone

	if err != nil {
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
		log.Logger.Error(
			"another daemon is already running",
			"pid", existing.PID,
			"port", existing.Port,
			"version", existing.Version,
		)
		return 1
	}

	svc.Server.SetDaemon(d)

	bannerInfo := buildBannerInfo(flags.Version, cfg.Addr)
	bannerInfo.PID = os.Getpid()

	svc.Server.OnListening = func(_ net.Addr) {
		logging.PrintBanner(os.Stderr, bannerInfo, log.UseColor)
	}

	exitCode := RunWithSignals(func(ctx context.Context) error {
		if err := d.Start(func() {}); err != nil {
			return fmt.Errorf("daemon start failed: %w", err)
		}
		return svc.Server.Run(ctx)
	}, log.Logger, flags.Version)

	d.Stop() //nolint:errcheck // best-effort cleanup
	return exitCode
}

// RunServer runs the gateway in non-daemon foreground mode.
func RunServer(flags Flags, cfg ConfigResult, svc Services, log LoggingResult) int {
	bannerInfo := buildBannerInfo(flags.Version, cfg.Addr)
	svc.Server.OnListening = func(_ net.Addr) {
		logging.PrintBanner(os.Stderr, bannerInfo, log.UseColor)
	}
	return RunWithSignals(func(ctx context.Context) error {
		return svc.Server.Run(ctx)
	}, log.Logger, flags.Version)
}

func buildBannerInfo(version, addr string) logging.BannerInfo {
	localAIBannerURL := modelrole.DefaultVllmBaseURL

	var localAIStatus string
	if isLocalAIReachable(localAIBannerURL) {
		localAIStatus = "online"
	} else {
		localAIStatus = "offline"
	}

	return logging.BannerInfo{
		Version:       version,
		Addr:          addr,
		LocalAIStatus: localAIStatus,
	}
}

// isLocalAIReachable checks if the local AI server responds to /models.
func isLocalAIReachable(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := httputil.NewClient(3 * time.Second).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
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
