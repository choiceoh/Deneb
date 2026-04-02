// Package provider contains LLM provider management for the gateway.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	// prewarmTimeout is the maximum time to wait for model prewarming.
	prewarmTimeout = 30 * time.Second
	// prewarmRetryDelay is the delay before retrying a failed prewarm.
	prewarmRetryDelay = 2 * time.Second
	// prewarmMaxRetries is the maximum number of prewarm attempts.
	prewarmMaxRetries = 2
	// prewarmDefaultBaseURL is the Google AI OpenAI-compatible endpoint.
	prewarmDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
)

// providerConfig holds credentials and endpoint for an LLM provider.
// Mirrors chat.ProviderConfig to avoid a cross-package dependency.
type providerConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
	API     string `json:"api"`
}

// PrewarmModel sends a minimal inference request to the primary model provider
// to trigger model loading and warm up inference caches. This is especially
// beneficial on DGX Spark with local GPU inference, where the first request
// can be significantly slower due to model loading.
//
// This function is designed to be called as a goroutine during gateway startup,
// before channel plugins begin accepting messages. Failures are logged but
// do not block startup.
func PrewarmModel(ctx context.Context, logger *slog.Logger) {
	providerID, modelName, cfg := loadPrewarmConfig(logger)
	if cfg == nil {
		logger.Info("model prewarm skipped: no provider config available")
		return
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = resolvePrewarmBaseURL(providerID)
	}
	client := llm.NewClient(baseURL, cfg.APIKey,
		llm.WithLogger(logger),
		llm.WithRetry(0, 0, 0), // No retries inside client; we handle retries here.
	)
	for attempt := 0; attempt <= prewarmMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(prewarmRetryDelay):
			case <-ctx.Done():
				return
			}
		}

		prewarmCtx, cancel := context.WithTimeout(ctx, prewarmTimeout)
		err := doPrewarmRequest(prewarmCtx, client, modelName)
		cancel()

		if err == nil {
			logger.Info("primary model prewarmed successfully")
			return
		}

		// Don't retry permanent errors (401, 403, etc.).
		var apiErr *llm.APIError
		if errors.As(err, &apiErr) && !httpretry.IsRetryable(apiErr.StatusCode) {
			logger.Warn("model prewarm failed with permanent error, skipping retries",
				"status", apiErr.StatusCode,
				"error", err,
			)
			return
		}

		logger.Warn("model prewarm returned error",
			"attempt", attempt+1,
			"error", err,
		)
	}

	logger.Warn("model prewarm exhausted all retries, continuing without warmup")
}

// doPrewarmRequest sends a minimal 1-token inference request and drains the
// streaming response.
func doPrewarmRequest(ctx context.Context, client *llm.Client, model string) error {
	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", "warmup")},
		MaxTokens: 1,
		Stream:    true,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return err
	}

	// Drain the stream to completion.
	for range events {
	}
	return nil
}

// loadPrewarmConfig reads deneb.json to find the default model and its
// provider config. Returns empty values if config is unavailable.
func loadPrewarmConfig(logger *slog.Logger) (providerID, modelName string, cfg *providerConfig) {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return "", "", nil
	}

	var root struct {
		Models struct {
			Providers map[string]providerConfig `json:"providers"`
		} `json:"models"`
		Agents struct {
			DefaultModel string          `json:"defaultModel"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("prewarm: failed to parse config", "error", err)
		return "", "", nil
	}

	// Resolve default model (e.g. "google/gemini-3.0-flash").
	model := root.Agents.DefaultModel
	if model == "" {
		model = extractModelFromDefaults(root.Agents.Defaults)
	}
	if model == "" {
		// No model configured; skip prewarm.
		return "", "", nil
	}

	// Split "provider/model" into parts.
	providerID, modelName = splitModelID(model)
	if providerID == "" || modelName == "" {
		return "", "", nil
	}

	// Look up provider config.
	pc, ok := root.Models.Providers[providerID]
	if !ok || pc.APIKey == "" {
		return "", "", nil
	}

	return providerID, modelName, &pc
}

// splitModelID splits "provider/model" into provider ID and model name.
func splitModelID(model string) (providerID, modelName string) {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i], model[i+1:]
	}
	return "", model
}

// extractModelFromDefaults handles both string and object forms of the
// agents.defaults.model field.
func extractModelFromDefaults(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var defaults struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(raw, &defaults); err != nil || len(defaults.Model) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if json.Unmarshal(defaults.Model, &s) == nil && s != "" {
		return s
	}
	// Try object with primary field.
	var obj struct {
		Primary string `json:"primary"`
	}
	if json.Unmarshal(defaults.Model, &obj) == nil && obj.Primary != "" {
		return obj.Primary
	}
	return ""
}

// resolvePrewarmBaseURL returns the default base URL for known providers.
func resolvePrewarmBaseURL(providerID string) string {
	switch providerID {
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "zai":
		return "https://api.z.ai/api/coding/paas/v4"
	default:
		return prewarmDefaultBaseURL
	}
}
