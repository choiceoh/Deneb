package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestDaemon_StartStop(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	d := NewDaemon(pidFile, 18789, "test", testLogger())

	if d.IsRunning() {
		t.Error("should not be running before start")
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(cancel); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if !d.IsRunning() {
		t.Error("should be running after start")
	}

	status := d.Status()
	if status.State != StateRunning {
		t.Errorf("expected running, got %s", status.State)
	}
	if status.Port != 18789 {
		t.Errorf("expected port 18789, got %d", status.Port)
	}
	if status.Version != "test" {
		t.Errorf("expected version 'test', got %s", status.Version)
	}

	// PID file should exist.
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("expected pid file: %v", err)
	}

	if err := d.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if d.IsRunning() {
		t.Error("should not be running after stop")
	}

	// PID file should be removed.
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("expected pid file to be removed")
	}
}

func TestDaemon_DoubleStart(t *testing.T) {
	dir := t.TempDir()
	d := NewDaemon(filepath.Join(dir, "test.pid"), 18789, "test", testLogger())

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.Start(cancel)
	err := d.Start(cancel)
	if err == nil {
		t.Error("expected error for double start")
	}
	d.Stop()
}

func TestDaemon_StopWhenNotRunning(t *testing.T) {
	dir := t.TempDir()
	d := NewDaemon(filepath.Join(dir, "test.pid"), 18789, "test", testLogger())

	err := d.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running daemon")
	}
}

func TestReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	d := NewDaemon(pidFile, 18789, "1.0.0", testLogger())
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(cancel)
	defer d.Stop()

	info, err := ReadPIDFile(pidFile)
	testutil.NoError(t, err)
	if info.Port != 18789 {
		t.Errorf("expected port 18789, got %d", info.Port)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", info.Version)
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), info.PID)
	}
}

func TestReadPIDFile_NotFound(t *testing.T) {
	_, err := ReadPIDFile("/nonexistent/path/pid.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCheckExistingDaemon_NoFile(t *testing.T) {
	dir := t.TempDir()
	d := NewDaemon(filepath.Join(dir, "test.pid"), 18789, "test", testLogger())
	if d.CheckExistingDaemon() != nil {
		t.Error("expected nil with no pid file")
	}
}

func TestCheckExistingDaemon_CurrentProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	d := NewDaemon(pidFile, 18789, "test", testLogger())
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(cancel)
	defer d.Stop()

	// Create a second daemon instance pointing to same PID file.
	d2 := NewDaemon(pidFile, 18789, "test", testLogger())
	info := d2.CheckExistingDaemon()
	if info == nil {
		t.Fatal("expected existing daemon info")
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), info.PID)
	}
}
