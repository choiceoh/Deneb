package bootstrap

import (
	"log/slog"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
)

// LoggingResult holds the resolved structured logger and formatting state.
type LoggingResult struct {
	Logger   *slog.Logger
	Format   string // "text" or "json"
	UseColor bool
}

// BuildEarlyLogger creates a minimal console-format logger for use before config loads.
func BuildEarlyLogger(flagLevel string) *slog.Logger {
	level := ParseLogLevel(flagLevel)
	return slog.New(logging.NewConsoleHandler(os.Stderr, &logging.ConsoleOptions{
		Level: level,
		Color: true,
	}))
}

// BuildLogger constructs the full structured logger from config and CLI flag overrides.
func BuildLogger(cfg *config.DenebConfig, flagLevel, flagFormat string) LoggingResult {
	resolvedLevel := "info"
	if cfg.Logging != nil && cfg.Logging.Level != "" {
		resolvedLevel = cfg.Logging.Level
	}
	if flagLevel != "" {
		resolvedLevel = flagLevel
	}
	level := ParseLogLevel(resolvedLevel)

	logFormat := "text"
	if cfg.Logging != nil && cfg.Logging.Format != "" {
		logFormat = cfg.Logging.Format
	}
	if flagFormat != "" {
		logFormat = flagFormat
	}

	var handler slog.Handler
	switch logFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	default:
		handler = logging.NewConsoleHandler(os.Stderr, &logging.ConsoleOptions{
			Level: level,
			Color: true,
		})
	}

	return LoggingResult{
		Logger:   slog.New(handler),
		Format:   logFormat,
		UseColor: logFormat != "json",
	}
}

// ParseLogLevel converts a log level string to slog.Level, defaulting to Info.
func ParseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
