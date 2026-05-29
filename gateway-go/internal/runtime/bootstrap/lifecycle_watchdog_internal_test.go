package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestRunWithSignals_HangingShutdown_ForcesExit verifies the shutdown watchdog:
// if graceful shutdown stalls after a signal, the process is force-exited (with
// the restart code for SIGUSR1) instead of wedging with the listener closed but
// the process alive. Uses the internal osExit / shutdownGraceTimeout hooks so it
// can assert the exit without terminating the test binary.
func TestRunWithSignals_HangingShutdown_ForcesExit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	origExit, origGrace := osExit, shutdownGraceTimeout
	t.Cleanup(func() { osExit, shutdownGraceTimeout = origExit, origGrace })

	shutdownGraceTimeout = 50 * time.Millisecond
	exited := make(chan int, 1)
	osExit = func(code int) { exited <- code } // capture instead of terminating

	release := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- RunWithSignals(func(ctx context.Context) error {
			// Request a restart, then simulate a shutdown that never finishes.
			_ = syscall.Kill(os.Getpid(), syscall.SIGUSR1)
			<-ctx.Done() // signal handler cancels the context
			<-release    // hang here — graceful shutdown stalls
			return nil
		}, logger)
	}()

	select {
	case code := <-exited:
		if code != ExitCodeRestart {
			t.Errorf("force-exit code = %d, want %d (ExitCodeRestart)", code, ExitCodeRestart)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not force-exit on a hung shutdown")
	}

	close(release) // let fn return so RunWithSignals can complete cleanly
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithSignals did not return after shutdown unblocked")
	}
}
