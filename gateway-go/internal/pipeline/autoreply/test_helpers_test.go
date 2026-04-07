package autoreply

import (
	"log/slog"
	"os"
)

// testSlogLogger returns a test logger that suppresses output below Error level.
func testSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
