package bootstrap_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/bootstrap"
)

func TestRunWithSignals_SIGUSR1_ReturnsRestartCode(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := bootstrap.RunWithSignals(func(ctx context.Context) error {
		go func() {
			time.Sleep(50 * time.Millisecond)
			syscall.Kill(os.Getpid(), syscall.SIGUSR1)
		}()
		<-ctx.Done()
		return nil
	}, logger)

	if exitCode != bootstrap.ExitCodeRestart {
		t.Errorf("exitCode = %d, want %d (ExitCodeRestart)", exitCode, bootstrap.ExitCodeRestart)
	}
}

func TestRunWithSignals_SIGTERM_ReturnsZero(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := bootstrap.RunWithSignals(func(ctx context.Context) error {
		go func() {
			time.Sleep(50 * time.Millisecond)
			syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
		<-ctx.Done()
		return nil
	}, logger)

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
}

func TestRunWithSignals_Error_ReturnsOne(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := bootstrap.RunWithSignals(func(_ context.Context) error {
		return errors.New("test error")
	}, logger)

	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}
