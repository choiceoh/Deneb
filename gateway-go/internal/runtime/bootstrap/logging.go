package bootstrap

import (
	"log/slog"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
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
		// Wire secret redaction into every JSON log attribute. The JSON
		// handler is what ships to /tmp/deneb-gateway.log and any downstream
		// aggregation, so this is the primary leak surface. No-op when
		// DENEB_REDACT_SECRETS=false was set at process start.
		opts := &slog.HandlerOptions{Level: level}
		opts.ReplaceAttr = redact.AttrReplacer(opts.ReplaceAttr)
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		// NOTE: the ConsoleHandler (dev TTY) does not accept a ReplaceAttr
		// hook today, so redaction is JSON-only for now. Dev-format logs
		// are not captured for long-term storage or forwarding, limiting
		// the blast radius. See follow-up below.
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
