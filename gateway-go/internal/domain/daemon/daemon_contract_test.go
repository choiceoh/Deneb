package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writePIDFixture(t *testing.T, path string, info PIDInfo) {
	t.Helper()
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewDaemonInitialStateAndNilLogger(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "daemon.pid")
	daemon := NewDaemon(pidFile, 1234, "v1", nil)
	if daemon.logger == nil || daemon.pidFile != pidFile || daemon.port != 1234 || daemon.version != "v1" {
		t.Fatalf("daemon = %+v", daemon)
	}
	status := daemon.Status()
	if status.State != StateIdle || status.PID != os.Getpid() || status.Port != 1234 || status.Version != "v1" ||
		status.UptimeMs != 0 || status.StartedAt != 0 || daemon.IsRunning() {
		t.Fatalf("initial status = %+v", status)
	}
}

func TestStartStopLifecycleErrorsCancellationAndRestart(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "nested", "daemon.pid")
	daemon := NewDaemon(pidFile, 1234, "v1", nil)
	if err := daemon.Stop(); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Stop idle = %v", err)
	}
	var cancels atomic.Int32
	if err := daemon.Start(func() { cancels.Add(1) }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := daemon.Start(func() {}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("duplicate Start = %v", err)
	}
	status := daemon.Status()
	if !daemon.IsRunning() || status.State != StateRunning || status.StartedAt <= 0 || status.UptimeMs < 0 {
		t.Fatalf("running status = %+v", status)
	}
	if err := daemon.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if cancels.Load() != 1 || daemon.Status().State != StateStopped || daemon.IsRunning() {
		t.Fatalf("stopped state cancels=%d status=%+v", cancels.Load(), daemon.Status())
	}
	if err := daemon.Stop(); err == nil {
		t.Fatal("duplicate Stop succeeded")
	}
	if err := daemon.Start(func() { cancels.Add(1) }); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := daemon.Stop(); err != nil || cancels.Load() != 2 {
		t.Fatalf("restart Stop=%v cancels=%d", err, cancels.Load())
	}
}

func TestStartWriteFailureTransitionsFailedAndCanRecover(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon := NewDaemon(filepath.Join(parentFile, "daemon.pid"), 1234, "v1", nil)
	if err := daemon.Start(func() {}); err == nil || !strings.Contains(err.Error(), "write pid file") {
		t.Fatalf("Start failure = %v", err)
	}
	if daemon.Status().State != StateFailed || daemon.IsRunning() {
		t.Fatalf("failed status = %+v", daemon.Status())
	}
	if err := daemon.Stop(); err == nil {
		t.Fatal("Stop failed daemon succeeded")
	}

	daemon.pidFile = filepath.Join(t.TempDir(), "daemon.pid")
	if err := daemon.Start(func() {}); err != nil {
		t.Fatalf("recovery Start: %v", err)
	}
	if err := daemon.Stop(); err != nil {
		t.Fatalf("recovery Stop: %v", err)
	}
}

func TestStopToleratesAlreadyMissingPIDFile(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "daemon.pid")
	daemon := NewDaemon(pidFile, 1, "v", nil)
	if err := daemon.Start(func() {}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(pidFile); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Stop(); err != nil {
		t.Fatalf("Stop missing PID: %v", err)
	}
	if daemon.Status().State != StateStopped {
		t.Fatalf("state = %q", daemon.Status().State)
	}
}

func TestReadPIDFileErrorsAndRoundTrip(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pid")
	if _, err := ReadPIDFile(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing error = %v", err)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.pid")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPIDFile(corrupt); err == nil || !strings.Contains(err.Error(), "parse pid file") {
		t.Fatalf("corrupt error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "valid.pid")
	want := PIDInfo{PID: 123, Port: 456, StartedAt: 789, Version: "v1"}
	writePIDFixture(t, path, want)
	got, err := ReadPIDFile(path)
	if err != nil || *got != want {
		t.Fatalf("ReadPIDFile = %+v,%v", got, err)
	}
}

func TestCheckExistingDaemonRejectsMalformedInvalidFutureAndStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	daemon := NewDaemon(path, 1, "v", nil)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if daemon.CheckExistingDaemon() != nil {
		t.Fatal("malformed PID file was accepted")
	}
	now := time.Now().UnixMilli()
	for _, tc := range []struct {
		name string
		info PIDInfo
	}{
		{name: "zero pid", info: PIDInfo{PID: 0, StartedAt: now}},
		{name: "negative pid", info: PIDInfo{PID: -2, StartedAt: now}},
		{name: "future", info: PIDInfo{PID: os.Getpid(), StartedAt: now + 61_000}},
		{name: "stale", info: PIDInfo{PID: os.Getpid(), StartedAt: now - 8*24*60*60*1000}},
		{name: "nonexistent", info: PIDInfo{PID: 1 << 30, StartedAt: now}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writePIDFixture(t, path, tc.info)
			if got := daemon.CheckExistingDaemon(); got != nil {
				t.Fatalf("accepted %+v", got)
			}
		})
	}
}

func TestCheckExistingDaemonCurrentProcessBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	daemon := NewDaemon(path, 1, "v", nil)
	now := time.Now().UnixMilli()
	for _, startedAt := range []int64{0, now, now + 59_000, now - 6*24*60*60*1000} {
		info := PIDInfo{PID: os.Getpid(), Port: 123, StartedAt: startedAt, Version: "v"}
		writePIDFixture(t, path, info)
		got := daemon.CheckExistingDaemon()
		if got == nil || *got != info {
			t.Fatalf("current process startedAt=%d got=%+v", startedAt, got)
		}
	}
}

func TestDaemonConcurrentStatusAndLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	daemon := NewDaemon(path, 1, "v", nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if i == 0 && j == 0 {
					_ = daemon.Start(func() {})
					continue
				}
				_ = daemon.Status()
				_ = daemon.IsRunning()
			}
		}(i)
	}
	wg.Wait()
	if daemon.IsRunning() {
		if err := daemon.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
}

func TestPIDInfoAndStatusJSONContracts(t *testing.T) {
	pid := PIDInfo{PID: 1, Port: 2, StartedAt: 3, Version: "버전"}
	data, err := json.Marshal(pid)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PIDInfo
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != pid {
		t.Fatalf("PIDInfo round trip = %+v err=%v", decoded, err)
	}
	status := StatusInfo{State: StateRunning, PID: 1, Port: 2, Version: "v", UptimeMs: 4, StartedAt: 3}
	data, _ = json.Marshal(status)
	if !strings.Contains(string(data), `"startedAt":3`) || !strings.Contains(string(data), `"uptimeMs":4`) {
		t.Fatalf("StatusInfo JSON = %s", data)
	}
}

func TestStartAcceptsContextCancelFunc(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	daemon := NewDaemon(filepath.Join(t.TempDir(), "daemon.pid"), 1, "v", nil)
	if err := daemon.Start(cancel); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not invoke context cancellation")
	}
}
