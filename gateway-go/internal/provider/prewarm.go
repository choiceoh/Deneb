// Package provider contains LLM provider management for the gateway.
package provider

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/bridge"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	// prewarmTimeout is the maximum time to wait for model prewarming.
	prewarmTimeout = 30 * time.Second
	// prewarmRetryDelay is the delay before retrying a failed prewarm.
	prewarmRetryDelay = 2 * time.Second
	// prewarmMaxRetries is the maximum number of prewarm attempts.
	prewarmMaxRetries = 2
)

// PrewarmModel sends a minimal inference request to the primary model provider
// to trigger model loading and warm up inference caches. This is especially
// beneficial on DGX Spark with local GPU inference, where the first request
// can be significantly slower due to model loading.
//
// This function is designed to be called as a goroutine during gateway startup,
// before channel plugins begin accepting messages. Failures are logged but
// do not block startup.
func PrewarmModel(ctx context.Context, host *bridge.PluginHost, logger *slog.Logger) {
	if host == nil {
		return
	}

	for attempt := 0; attempt <= prewarmMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(prewarmRetryDelay):
			case <-ctx.Done():
				return
			}
		}

		prewarmCtx, cancel := context.WithTimeout(ctx, prewarmTimeout)
		params, _ := json.Marshal(map[string]any{
			"prompt":    "warmup",
			"maxTokens": 1,
		})
		resp, err := host.Forward(prewarmCtx, &protocol.RequestFrame{
			Type:   "req",
			ID:     "go-model-prewarm",
			Method: "provider.prewarm",
			Params: params,
		})
		cancel()

		if err != nil {
			logger.Warn("model prewarm attempt failed",
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		if resp != nil && resp.OK {
			logger.Info("primary model prewarmed successfully")
			return
		}

		if resp != nil && resp.Error != nil {
			logger.Warn("model prewarm returned error",
				"attempt", attempt+1,
				"error", resp.Error,
			)
		}
	}

	logger.Warn("model prewarm exhausted all retries, continuing without warmup")
}
