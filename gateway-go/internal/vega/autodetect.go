package vega

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// ShouldEnableVega determines whether the Vega backend should be activated.
// In local AI mode, checks if the local AI server is reachable.
// Falls back to FTS-only mode if local AI is unavailable but FFI is present.
func ShouldEnableVega(ffiAvailable bool, localAIURL string, logger *slog.Logger) bool {
	if !ffiAvailable {
		if logger != nil {
			logger.Debug("vega: FFI not available, skipping activation")
		}
		return false
	}

	// Vega FTS (non-ML) always works with FFI, so enable regardless of local AI.
	return true
}

// IsLocalAIReachable checks if the local AI server responds to /v1/models.
func IsLocalAIReachable(baseURL string) bool {
	if baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// IsRerankerReachable checks if the local reranker server is responsive.
func IsRerankerReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultRerankURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// Any response (even 405) means the server is running.
	return true
}

// GetJinaAPIKey reads the Jina AI API key from the JINA_API_KEY environment variable.
// Returns empty string if not configured (reranking will be disabled).
func GetJinaAPIKey() string {
	return os.Getenv("JINA_API_KEY")
}
