package provider

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestPrewarmModel_NilHost(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Should not panic with nil host.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	PrewarmModel(ctx, nil, logger)
}

func TestPrewarmModel_ContextCanceled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.
	PrewarmModel(ctx, nil, logger)
}
