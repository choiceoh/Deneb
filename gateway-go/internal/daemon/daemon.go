// Package daemon manages the gateway daemon process lifecycle.
//
// This mirrors the daemon management in src/daemon/ from the TypeScript codebase.
// The daemon handles background process supervision, PID file management,
// and graceful restart coordination.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// State represents the daemon lifecycle state.
type State string

const (
	StateIdle     State = "idle"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// PIDInfo is written to the PID file for process identification.
type PIDInfo struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt int64  `json:"startedAt"`
	Version   string `json:"version"`
}

// StatusInfo describes the current daemon state.
type StatusInfo struct {
	State     State  `json:"state"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	UptimeMs  int64  `json:"uptimeMs"`
	StartedAt int64  `json:"startedAt,omitempty"`
}

// Daemon manages the gateway process lifecycle.
type Daemon struct {
	mu        sync.RWMutex
	state     State
	pidFile   string
	port      int
	version   string
	startedAt time.Time
	logger    *slog.Logger
	stopFn    context.CancelFunc
}

// NewDaemon creates a new daemon manager.
func NewDaemon(pidFile string, port int, version string, logger *slog.Logger) *Daemon {
	return &Daemon{
		state:   StateIdle,
		pidFile: pidFile,
		port:    port,
		version: version,
		logger:  logger,
	}
}

// Start transitions the daemon to running state and writes the PID file.
func (d *Daemon) Start(cancel context.CancelFunc) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateRunning {
		return fmt.Errorf("daemon already running")
	}

	d.state = StateStarting
	d.startedAt = time.Now()
	d.stopFn = cancel

	info := PIDInfo{
		PID:       os.Getpid(),
		Port:      d.port,
		StartedAt: d.startedAt.UnixMilli(),
		Version:   d.version,
	}

	if err := d.writePIDFile(info); err != nil {
		d.state = StateFailed
		return fmt.Errorf("write pid file: %w", err)
	}

	d.state = StateRunning
	d.logger.Info("daemon started", "pid", info.PID, "port", d.port)
	return nil
}

// Stop transitions the daemon to stopped state and removes the PID file.
func (d *Daemon) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state != StateRunning && d.state != StateStarting {
		return fmt.Errorf("daemon not running (state=%s)", d.state)
	}

	d.state = StateStopping
	if d.stopFn != nil {
		d.stopFn()
	}

	if err := d.removePIDFile(); err != nil {
		d.logger.Warn("failed to remove pid file", "error", err)
	}

	d.state = StateStopped
	d.logger.Info("daemon stopped")
	return nil
}

// Status returns the current daemon state.
func (d *Daemon) Status() StatusInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info := StatusInfo{
		State:   d.state,
		PID:     os.Getpid(),
		Port:    d.port,
		Version: d.version,
	}

	if d.state == StateRunning {
		info.UptimeMs = time.Since(d.startedAt).Milliseconds()
		info.StartedAt = d.startedAt.UnixMilli()
	}

	return info
}

// IsRunning returns true if the daemon is in running state.
func (d *Daemon) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state == StateRunning
}

// CheckExistingDaemon reads the PID file to check if another daemon is running.
// Returns the PID info if a live process owns the PID file, nil otherwise.
func (d *Daemon) CheckExistingDaemon() *PIDInfo {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return nil
	}

	var info PIDInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}

	// Check if the process is still alive.
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		return nil
	}

	// Signal 0 checks for process existence without sending a signal.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process doesn't exist; stale PID file.
		return nil
	}

	return &info
}

func (d *Daemon) writePIDFile(info PIDInfo) error {
	dir := filepath.Dir(d.pidFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(d.pidFile, data, 0o644)
}

func (d *Daemon) removePIDFile() error {
	return os.Remove(d.pidFile)
}

// ReadPIDFile reads and parses a PID file.
func ReadPIDFile(path string) (*PIDInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info PIDInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse pid file: %w", err)
	}
	return &info, nil
}

// FormatPID returns a human-readable PID description.
func FormatPID(pid int) string {
	return strconv.Itoa(pid)
}
