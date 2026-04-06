package bootstrap

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/reranker"
	"github.com/choiceoh/deneb/gateway-go/internal/server"
)

// Services holds the wired server and supporting service state.
type Services struct {
	Server *server.Server
}

// WireServices assembles the gateway server with all backing services:
// embedding provider and Jina reranker.
func WireServices(addr string, rtCfg *config.GatewayRuntimeConfig, logger *slog.Logger, version string, useColor bool) (Services, error) {
	embedder := newEmbedder(logger)
	jinaKey := reranker.GetJinaAPIKey()

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

	return Services{
		Server: srv,
	}, nil
}

// newEmbedder creates the embedding provider.
// Prefers local GGUF model (DENEB_EMBED_MODEL) over Gemini API.
func newEmbedder(logger *slog.Logger) embedding.Embedder {
	if modelPath := os.Getenv("DENEB_EMBED_MODEL"); modelPath != "" {
		if _, err := os.Stat(modelPath); err == nil {
			if !ffi.MLAvailable() {
				logger.Info("embedding: DENEB_EMBED_MODEL set but ML feature not compiled, falling back", "path", modelPath)
			} else {
				logger.Info("embedding: using local GGUF model", "path", modelPath)
				return embedding.NewLocalEmbedder(modelPath, logger)
			}
		} else {
			logger.Warn("embedding: DENEB_EMBED_MODEL set but file not found, falling back", "path", modelPath)
		}
	}
	if e := embedding.NewGeminiEmbedder(os.Getenv("GEMINI_API_KEY"), logger); e != nil {
		return e
	}
	return nil
}
