package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// Option configures the gateway server.
type Option func(*Server)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithVersion sets the server version string.
func WithVersion(version string) Option {
	return func(s *Server) {
		s.version = version
	}
}

// WithConfig sets the resolved runtime configuration.
func WithConfig(cfg *config.GatewayRuntimeConfig) Option {
	return func(s *Server) {
		s.runtimeCfg = cfg
	}
}

// WithLogColor enables ANSI color in startup/shutdown banners.
func WithLogColor(color bool) Option {
	return func(s *Server) {
		s.logColor = color
	}
}

// RuntimeConfig returns the server's runtime configuration (may be nil if not set).
func (s *Server) RuntimeConfig() *config.GatewayRuntimeConfig {
	return s.runtimeCfg
}

// WithProviders sets the provider plugin registry.
func WithProviders(r *provider.Registry) Option {
	return func(s *Server) {
		s.providers = r
	}
}

// WithTranscript sets the session transcript writer.
func WithTranscript(w *transcript.Writer) Option {
	return func(s *Server) {
		s.transcript = w
	}
}
