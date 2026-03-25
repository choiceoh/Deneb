// Package bridge — spawn.go manages the lifecycle of the Node.js Plugin Host
// child process. The Go gateway spawns Node.js, which listens on a Unix domain
// socket, then connects the bridge for RPC forwarding.
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
)

const (
	// socketWaitTimeout is how long to wait for the Plugin Host socket to appear.
	socketWaitTimeout = timeouts.SocketWait
	// socketPollInterval is the polling interval while waiting for the socket.
	socketPollInterval = 100 * time.Millisecond
	// respawnDelay is the backoff before restarting a crashed Plugin Host.
	respawnDelay = 2 * time.Second
)

// SpawnConfig configures the Plugin Host child process.
type SpawnConfig struct {
	// Command is the shell command to run the Plugin Host (e.g. "node dist/plugin-host/main.js").
	Command string
	// SocketPath is the Unix socket path for IPC. Auto-generated if empty.
	SocketPath string
	// Env is additional environment variables for the child process.
	Env map[string]string
	// Logger for spawn-related messages.
	Logger *slog.Logger
}

// SpawnResult holds the running Plugin Host and its bridge connection.
type SpawnResult struct {
	Host   *PluginHost
	Cmd    *exec.Cmd
	Cancel context.CancelFunc
}

// SpawnPluginHost starts the Node.js Plugin Host as a child process, waits for
// its Unix socket to become available, and returns a connected bridge.
func SpawnPluginHost(ctx context.Context, cfg SpawnConfig) (*SpawnResult, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("plugin host command is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = fmt.Sprintf("/tmp/deneb-plugin-host-%d.sock", os.Getpid())
	}

	// Remove stale socket file if it exists.
	_ = os.Remove(socketPath)

	// Parse command into argv.
	args := strings.Fields(cfg.Command)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty plugin host command")
	}

	spawnCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(spawnCtx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Build environment: inherit current env + socket path + extras.
	env := os.Environ()
	env = append(env, fmt.Sprintf("DENEB_PLUGIN_HOST_SOCKET=%s", socketPath))
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	cfg.Logger.Info("spawning plugin host",
		"command", cfg.Command,
		"socket", socketPath,
	)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start plugin host: %w", err)
	}

	cfg.Logger.Info("plugin host process started", "pid", cmd.Process.Pid)

	// Wait for the socket file to appear and accept connections.
	if err := waitForSocket(spawnCtx, socketPath, cfg.Logger); err != nil {
		// Kill the child if it didn't create the socket in time.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cancel()
		return nil, fmt.Errorf("plugin host socket not ready: %w", err)
	}

	// Connect the bridge.
	host := NewWithSocket(socketPath, cfg.Logger)
	connectCtx, connectCancel := context.WithTimeout(spawnCtx, 5*time.Second)
	err := host.ConnectWithReconnect(connectCtx)
	connectCancel()
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cancel()
		return nil, fmt.Errorf("connect to plugin host: %w", err)
	}

	cfg.Logger.Info("plugin host bridge connected", "socket", socketPath, "pid", cmd.Process.Pid)

	// Monitor child process in the background.
	go monitorChild(spawnCtx, cmd, cfg.Logger)

	return &SpawnResult{
		Host:   host,
		Cmd:    cmd,
		Cancel: cancel,
	}, nil
}

// waitForSocket polls until the Unix socket is connectable or the context expires.
func waitForSocket(ctx context.Context, socketPath string, logger *slog.Logger) error {
	deadline := time.Now().Add(socketWaitTimeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for socket at %s", socketPath)
		default:
		}

		// Check if socket file exists first.
		if _, err := os.Stat(socketPath); err != nil {
			time.Sleep(socketPollInterval)
			continue
		}

		// Try connecting to verify the socket is listening.
		conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
		if err != nil {
			time.Sleep(socketPollInterval)
			continue
		}
		_ = conn.Close()
		logger.Debug("plugin host socket ready", "path", socketPath)
		return nil
	}
}

// monitorChild watches the child process and logs when it exits.
func monitorChild(ctx context.Context, cmd *exec.Cmd, logger *slog.Logger) {
	err := cmd.Wait()
	select {
	case <-ctx.Done():
		// Expected shutdown — parent canceled.
		logger.Info("plugin host stopped (parent shutdown)", "pid", cmd.Process.Pid)
	default:
		if err != nil {
			logger.Error("plugin host exited unexpectedly", "pid", cmd.Process.Pid, "error", err)
		} else {
			logger.Warn("plugin host exited cleanly but unexpectedly", "pid", cmd.Process.Pid)
		}
	}
}

// Shutdown gracefully stops the Plugin Host: closes the bridge, then signals the child.
func (r *SpawnResult) Shutdown() error {
	if r.Host != nil {
		_ = r.Host.Close()
	}
	if r.Cancel != nil {
		r.Cancel()
	}
	if r.Cmd != nil && r.Cmd.Process != nil {
		// Send SIGTERM first, then wait briefly.
		_ = r.Cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- r.Cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = r.Cmd.Process.Kill()
			<-done
		}
	}
	return nil
}
