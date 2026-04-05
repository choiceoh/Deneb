package bootstrap

import (
	"fmt"
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
func WireServices(addr string, rtCfg *config.GatewayRuntimeConfig, logger *slog.Logger, version string, useColor bool) (Services, error) {
	geminiEmbedder := newGeminiEmbedder(logger)
	jinaKey := vega.GetJinaAPIKey()

	srv, err := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(version),
		server.WithConfig(rtCfg),
		server.WithLogColor(useColor),
		server.WithGeminiEmbedder(geminiEmbedder),
		server.WithJinaAPIKey(jinaKey),
	)
	if err != nil {
		return Services{}, fmt.Errorf("server init: %w", err)
	}

	vegaEnabled := initVega(srv, logger, geminiEmbedder)

	return Services{
		Server:      srv,
		VegaEnabled: vegaEnabled,
	}, nil
}

func newGeminiEmbedder(logger *slog.Logger) *embedding.GeminiEmbedder {
	return embedding.NewGeminiEmbedder(os.Getenv("GEMINI_API_KEY"), logger)
}

// initVega configures the Vega search backend with Gemini embedding,
// lightweight model query expansion, and Jina reranking. Returns false if unavailable.
func initVega(srv *server.Server, logger *slog.Logger, embedder *embedding.GeminiEmbedder) bool {
	lwURL := modelrole.DefaultLocalAIBaseURL
	lwModel := modelrole.DefaultLocalAIModel

	if !vega.ShouldEnableVega(ffi.Available, lwURL, logger) {
		logger.Info("vega: disabled (FFI not available)")
		return false
	}

	cfg := vega.EnhancedBackendConfig{
		Logger:      logger,
		LocalAIURL:   lwURL,
		LocalAIModel: lwModel,
		Embedder:    embedder,
		JinaAPIKey:  vega.GetJinaAPIKey(),
	}

	backend := vega.NewEnhancedBackend(cfg)
	srv.SetVega(backend)
	return true
}
