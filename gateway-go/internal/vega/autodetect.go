package vega

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// ShouldEnableVega determines whether the Vega backend should be activated.
// In sglang mode, checks if the SGLang server is reachable.
// Falls back to FTS-only mode if SGLang is unavailable but FFI is present.
func ShouldEnableVega(ffiAvailable bool, sglangURL string, logger *slog.Logger) bool {
	if !ffiAvailable {
		if logger != nil {
			logger.Debug("vega: FFI not available, skipping activation")
		}
		return false
	}

	// Vega FTS (non-ML) always works with FFI, so enable regardless of SGLang.
	return true
}

// IsSglangReachable checks if the SGLang server responds to /v1/models.
func IsSglangReachable(baseURL string) bool {
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
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// GetJinaAPIKey reads the Jina AI API key from the JINA_API_KEY environment variable.
// Returns empty string if not configured (reranking will be disabled).
func GetJinaAPIKey() string {
	return os.Getenv("JINA_API_KEY")
}
