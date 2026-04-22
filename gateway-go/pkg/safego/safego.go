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
// logger with the given name as context; it does not propagate.
// If logger is nil, panics are silently swallowed — callers are strongly
// encouraged to pass a real logger.
func Go(logger Logger, name string, fn func()) {
	go func() {
		defer recoverPanic(logger, name)
		fn()
	}()
}

// GoWithSlog is a convenience wrapper for callers that already have
// a *slog.Logger handy and do not want to pass the Logger interface manually.
func GoWithSlog(logger *slog.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Error("panic in background goroutine", "goroutine", name, "panic", r)
				}
			}
		}()
		fn()
	}()
}

func recoverPanic(logger Logger, name string) {
	if r := recover(); r != nil {
		if logger != nil {
			logger.Error("panic in background goroutine", "goroutine", name, "panic", r)
		}
	}
}
