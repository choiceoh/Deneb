package bootstrap

import (
	"log/slog"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// Services holds the wired server and supporting service state.
type Services struct {
	Server      *server.Server
	VegaEnabled bool
}

// WireServices assembles the gateway server with all backing services:
// Gemini embedder, Vega search backend, and Jina reranker.
func WireServices(addr string, rtCfg *config.GatewayRuntimeConfig, logger *slog.Logger, version string, useColor bool) Services {
	geminiEmbedder := newGeminiEmbedder(logger)
	jinaKey := vega.GetJinaAPIKey()

	srv := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(version),
		server.WithConfig(rtCfg),
		server.WithLogColor(useColor),
		server.WithGeminiEmbedder(geminiEmbedder),
		server.WithJinaAPIKey(jinaKey),
	)

	vegaEnabled := initVega(srv, logger, geminiEmbedder)

	return Services{
		Server:      srv,
		VegaEnabled: vegaEnabled,
	}
}

func newGeminiEmbedder(logger *slog.Logger) *embedding.GeminiEmbedder {
	embedder := embedding.NewGeminiEmbedder(os.Getenv("GEMINI_API_KEY"), logger)
	if embedder != nil {
		logger.Info("gemini: embedding enabled (gemini-embedding-2-preview)")
	} else {
		logger.Info("gemini: embedding disabled (GEMINI_API_KEY not set)")
	}
	return embedder
}

// initVega configures the Vega search backend with Gemini embedding,
// lightweight model query expansion, and Jina reranking. Returns false if unavailable.
func initVega(srv *server.Server, logger *slog.Logger, embedder *embedding.GeminiEmbedder) bool {
	lwURL := modelrole.DefaultSglangBaseURL
	lwModel := modelrole.DefaultSglangModel

	if !vega.ShouldEnableVega(ffi.Available, lwURL, logger) {
		logger.Info("vega: disabled (FFI not available)")
		return false
	}

	cfg := vega.EnhancedBackendConfig{
		Logger:      logger,
		SglangURL:   lwURL,
		SglangModel: lwModel,
		Embedder:    embedder,
		JinaAPIKey:  vega.GetJinaAPIKey(),
	}

	backend := vega.NewEnhancedBackend(cfg)
	srv.SetVega(backend)
	return true
}
