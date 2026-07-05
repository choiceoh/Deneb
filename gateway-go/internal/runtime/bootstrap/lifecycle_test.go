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
	}, logger, "")

	if exitCode != bootstrap.ExitCodeRestart {
		t.Errorf("exitCode = %d, want %d (ExitCodeRestart)", exitCode, bootstrap.ExitCodeRestart)
	}
}

// TestRunWithSignals_RefusedUSR1_KeepsServing drills the downgrade guard end to
// end in-process: with a versioned build (guard ON) the candidate binary is
// this test binary, which cannot answer --print-version — definitionally a
// stale pre-guard build — so SIGUSR1 must be REFUSED (context stays alive, no
// restart code) and a later SIGTERM must still shut down cleanly.
func TestRunWithSignals_RefusedUSR1_KeepsServing(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 4}))

	refused := make(chan bool, 1)
	exitCode := bootstrap.RunWithSignals(func(ctx context.Context) error {
		go func() {
			syscall.Kill(os.Getpid(), syscall.SIGUSR1)
			time.Sleep(300 * time.Millisecond) // guard probes + refuses
			select {
			case <-ctx.Done():
				refused <- false // restart proceeded — guard failed
			default:
				refused <- true // still serving after refused SIGUSR1
			}
			syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
		<-ctx.Done()
		return nil
	}, logger, "9.9.9")

	if ok := <-refused; !ok {
		t.Error("SIGUSR1 into a version-less candidate must be refused (context cancelled)")
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 (SIGTERM shutdown, not restart)", exitCode)
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
	}, logger, "")

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
}

func TestRunWithSignals_Error_ReturnsOne(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exitCode := bootstrap.RunWithSignals(func(_ context.Context) error {
		return errors.New("test error")
	}, logger, "")

	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}
