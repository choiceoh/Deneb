package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// ServerManager manages a persistent server process across iterations.
// Instead of starting/stopping a server for every metric run, it keeps
// the server alive and only restarts when target files change.
type ServerManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	healthURL string
	startup   time.Duration
	lastHash  string // content hash when server was last (re)started
	workdir   string
	serverCmd string
	logPath   string   // path to server log file
	logFile   *os.File // open log file handle, closed on stop
	logger    *slog.Logger
}

// NewServerManager creates a server manager. Does not start the server.
func NewServerManager(logger *slog.Logger) *ServerManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServerManager{logger: logger.With("component", "server-manager")}
}

// Start launches the server process and waits for the health check to pass.
func (sm *ServerManager) Start(workdir, serverCmd, healthURL string, startupSec int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.workdir = workdir
	sm.serverCmd = serverCmd
	sm.healthURL = healthURL
	sm.startup = time.Duration(startupSec) * time.Second

	return sm.startLocked()
}

func (sm *ServerManager) startLocked() error {
	sm.logger.Info("starting server", "cmd", sm.serverCmd, "workdir", sm.workdir)

	cmd := exec.Command("bash", "-c", sm.serverCmd)
	cmd.Dir = sm.workdir
	cmd.Env = os.Environ()

	// Use process group so we can kill the entire tree (bash + child server).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Redirect server output to a log file instead of mixing with main process.
	sm.logPath = filepath.Join(sm.workdir, ".autoresearch", "server.log")
	os.MkdirAll(filepath.Dir(sm.logPath), 0o755)
	logFile, err := os.OpenFile(sm.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		sm.logger.Warn("failed to create server log, using discard", "error", err)
		cmd.Stdout = nil
		cmd.Stderr = nil
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("start server: %w", err)
	}
	sm.cmd = cmd
	sm.logFile = logFile

	// Wait for health check.
	if sm.healthURL != "" {
		if err := sm.waitHealth(); err != nil {
			sm.stopLocked()
			return fmt.Errorf("server health check failed: %w", err)
		}
	}

	sm.logger.Info("server started successfully", "log", sm.logPath)
	return nil
}

func (sm *ServerManager) waitHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), sm.startup)
	defer cancel()
	return httputil.WaitForHealth(ctx, sm.healthURL, 500*time.Millisecond)
}

// RestartIfNeeded restarts the server only when the content hash changed.
// Returns true if the server was restarted.
func (sm *ServerManager) RestartIfNeeded(currentHash string) (bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.cmd == nil {
		return false, fmt.Errorf("server not started")
	}

	if sm.lastHash == currentHash {
		return false, nil
	}

	sm.logger.Info("code changed, restarting server", "old_hash", sm.lastHash, "new_hash", currentHash)
	sm.stopLocked()
	if err := sm.startLocked(); err != nil {
		return false, err
	}
	sm.lastHash = currentHash
	return true, nil
}

// SetHash records the content hash without restarting.
func (sm *ServerManager) SetHash(hash string) {
	sm.mu.Lock()
	sm.lastHash = hash
	sm.mu.Unlock()
}

// Stop kills the server process.
func (sm *ServerManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.stopLocked()
}

func (sm *ServerManager) stopLocked() {
	if sm.cmd == nil || sm.cmd.Process == nil {
		return
	}
	sm.logger.Info("stopping server")

	// Kill the entire process group (server + any children spawned by bash -c).
	pgid, err := syscall.Getpgid(sm.cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL) // best-effort: process may already be dead
	} else {
		_ = sm.cmd.Process.Kill() // best-effort: process may already be dead
	}
	_ = sm.cmd.Wait() // best-effort: reap zombie
	sm.cmd = nil

	// Close the log file to avoid descriptor leaks across restarts.
	if sm.logFile != nil {
		sm.logFile.Close()
		sm.logFile = nil
	}
}

// IsRunning returns true if the server process is alive.
func (sm *ServerManager) IsRunning() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.cmd == nil || sm.cmd.Process == nil {
		return false
	}
	// Check if process has exited by sending signal 0.
	err := sm.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

// URL returns the health URL, which metric scripts use as server address.
func (sm *ServerManager) URL() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.healthURL
}
