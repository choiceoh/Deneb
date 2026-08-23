// Package safego provides small helpers for spawning long-lived goroutines
// with built-in panic recovery. Use it whenever a background loop (periodic
// task, event subscriber, poll loop) could otherwise bring down the whole
// process on an unhandled panic.
package safego

import (
	"log/slog"
)

// Logger is the minimal logging contract safego needs. *slog.Logger satisfies it.
type Logger interface {
	Error(msg string, args ...any)
}

// Go runs fn in a new goroutine with panic recovery. A panic is logged via
// logger with the given name as context; it does not propagate. A nil logger
// falls back to slog.Default() — a recovered panic is never swallowed, because
// a goroutine that died silently is exactly the failure nobody can diagnose.
func Go(logger Logger, name string, fn func()) {
	go func() {
		defer recoverPanic(logger, name)
		fn()
	}()
}

// GoWithSlog is a convenience wrapper for callers that already have
// a *slog.Logger handy and do not want to pass the Logger interface manually.
// A nil logger falls back to slog.Default() like Go.
func GoWithSlog(logger *slog.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicLogger(logger).Error("panic in background goroutine", "goroutine", name, "panic", r)
			}
		}()
		fn()
	}()
}

func recoverPanic(logger Logger, name string) {
	if r := recover(); r != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("panic in background goroutine", "goroutine", name, "panic", r)
	}
}

// panicLogger returns logger, or the process default when the caller passed
// nil — the recovery path must always have somewhere to report.
func panicLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
