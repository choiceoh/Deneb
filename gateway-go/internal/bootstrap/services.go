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
// embedding provider, Vega search backend, and Jina reranker.
func WireServices(addr string, rtCfg *config.GatewayRuntimeConfig, logger *slog.Logger, version string, useColor bool) (Services, error) {
	embedder := newEmbedder(logger)
	jinaKey := vega.GetJinaAPIKey()

	srv, err := server.New(addr,
		server.WithLogger(logger),
		server.WithVersion(version),
		server.WithConfig(rtCfg),
		server.WithLogColor(useColor),
		server.WithEmbedder(embedder),
		server.WithJinaAPIKey(jinaKey),
	)
	if err != nil {
		return Services{}, fmt.Errorf("server init: %w", err)
	}

	vegaEnabled := initVega(srv, logger, embedder)

	return Services{
		Server:      srv,
		VegaEnabled: vegaEnabled,
	}, nil
}

// newEmbedder creates the embedding provider.
// Prefers local GGUF model (DENEB_EMBED_MODEL) over Gemini API.
func newEmbedder(logger *slog.Logger) embedding.Embedder {
	if modelPath := os.Getenv("DENEB_EMBED_MODEL"); modelPath != "" {
		if _, err := os.Stat(modelPath); err == nil {
			logger.Info("embedding: using local GGUF model", "path", modelPath)
			return embedding.NewLocalEmbedder(modelPath, logger)
		}
		logger.Warn("embedding: DENEB_EMBED_MODEL set but file not found, falling back", "path", modelPath)
	}
	if e := embedding.NewGeminiEmbedder(os.Getenv("GEMINI_API_KEY"), logger); e != nil {
		return e
	}
	return nil
}

// initVega configures the Vega search backend with embedding,
// lightweight model query expansion, and Jina reranking. Returns false if unavailable.
func initVega(srv *server.Server, logger *slog.Logger, embedder embedding.Embedder) bool {
	lwURL := modelrole.DefaultVllmBaseURL
	lwModel := modelrole.DefaultVllmModel

	if !vega.ShouldEnableVega(ffi.Available, lwURL, logger) {
		logger.Info("vega: disabled (FFI not available)")
		return false
	}

	cfg := vega.EnhancedBackendConfig{
		Logger:       logger,
		LocalAIURL:   lwURL,
		LocalAIModel: lwModel,
		Embedder:     embedder,
		JinaAPIKey:   vega.GetJinaAPIKey(),
	}

	backend := vega.NewEnhancedBackend(cfg)
	srv.SetVega(backend)
	return true
}
