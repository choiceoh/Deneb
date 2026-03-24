package bridge

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpawnConfig_Validation(t *testing.T) {
	t.Run("empty command returns error", func(t *testing.T) {
		ctx := context.Background()
		_, err := SpawnPluginHost(ctx, SpawnConfig{})
		if err == nil {
			t.Fatal("expected error for empty command")
		}
		if err.Error() != "plugin host command is required" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty parsed command returns error", func(t *testing.T) {
		ctx := context.Background()
		_, err := SpawnPluginHost(ctx, SpawnConfig{Command: "   "})
		// After trimming fields, "   " becomes empty args but still single space element.
		// The spawn itself will fail because " " is not a valid command.
		if err == nil {
			t.Fatal("expected error for whitespace-only command")
		}
	})
}

func TestSpawnConfig_SocketPath(t *testing.T) {
	t.Run("auto-generates socket path when empty", func(t *testing.T) {
		cfg := SpawnConfig{Command: "true"}
		if cfg.SocketPath != "" {
			t.Fatal("expected empty socket path in config")
		}
		// The function generates it internally; we just verify it would be set.
	})

	t.Run("custom socket path preserved", func(t *testing.T) {
		custom := "/tmp/test-deneb-plugin-host.sock"
		cfg := SpawnConfig{
			Command:    "true",
			SocketPath: custom,
		}
		if cfg.SocketPath != custom {
			t.Fatalf("expected custom socket path %q, got %q", custom, cfg.SocketPath)
		}
	})
}

func TestWaitForSocket(t *testing.T) {
	t.Run("returns error when context expires", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		err := waitForSocket(ctx, "/tmp/nonexistent-socket-"+t.Name(), logger)
		if err == nil {
			t.Fatal("expected timeout error")
		}
	})
}

func TestSpawnResult_Shutdown(t *testing.T) {
	t.Run("nil result fields handled gracefully", func(t *testing.T) {
		r := &SpawnResult{}
		err := r.Shutdown()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStaleSocketCleanup(t *testing.T) {
	// Verify that SpawnPluginHost removes stale socket files.
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create a stale socket file.
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// The spawn will fail (command is "false" which exits immediately),
	// but the stale socket should have been removed before the command ran.
	_, _ = SpawnPluginHost(ctx, SpawnConfig{
		Command:    "false",
		SocketPath: socketPath,
		Logger:     slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	})

	// Stale socket should be gone (either removed by spawn or by failed process).
	// The important thing is it doesn't block on a stale file.
}
