package vega

import (
	"context"
	"encoding/json"
	"io"
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
	if logger != nil {
		logger.Info("vega: FFI available, enabling Vega")
	}
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

// EmbedEndpoint holds the auto-detected embedding server URL and model name.
type EmbedEndpoint struct {
	URL   string // e.g. "http://127.0.0.1:30001/v1"
	Model string // e.g. "BAAI/bge-m3"
}

// defaultEmbedCandidates lists ports to probe for an embedding server on localhost.
// Port 30001 is the conventional DGX Spark embedding port.
var defaultEmbedCandidates = []string{
	"http://127.0.0.1:30001/v1",
	"http://127.0.0.1:30002/v1",
}

// EmbedDetectResult holds the auto-detection result including an optional
// server handle that must be stopped on gateway shutdown.
type EmbedDetectResult struct {
	Endpoint *EmbedEndpoint
	Server   *EmbedServer // non-nil if we auto-launched the server
}

// DetectOrLaunchEmbedServer auto-detects a running embedding server, or
// launches one if a GPU is available (DGX Spark).
// Priority: env vars → probe default ports → auto-launch on GPU hosts.
// Caller must call result.Server.Stop() on shutdown if Server is non-nil.
func DetectOrLaunchEmbedServer(logger *slog.Logger) *EmbedDetectResult {
	// 1. Explicit env var override — skip probing.
	envURL, envModel := getEmbedEnv()
	if envURL != "" && envModel != "" {
		if logger != nil {
			logger.Info("vega: using explicit embedding endpoint",
				"url", envURL, "model", envModel)
		}
		return &EmbedDetectResult{
			Endpoint: &EmbedEndpoint{URL: envURL, Model: envModel},
		}
	}

	// 2. Probe default candidate ports for a running embedding server.
	for _, candidate := range defaultEmbedCandidates {
		if model := probeEmbedModel(candidate); model != "" {
			if logger != nil {
				logger.Info("vega: auto-detected embedding server",
					"url", candidate, "model", model)
			}
			return &EmbedDetectResult{
				Endpoint: &EmbedEndpoint{URL: candidate, Model: model},
			}
		}
	}

	// 3. No running server found. If we have a GPU, auto-launch one.
	if HasGPU() {
		if logger != nil {
			logger.Info("vega: GPU detected, auto-launching embedding server")
		}
		srv, ep := LaunchEmbedServer(EmbedServerConfig{Logger: logger})
		if ep != nil {
			return &EmbedDetectResult{Endpoint: ep, Server: srv}
		}
	}

	if logger != nil {
		logger.Info("vega: no embedding server detected, embedding disabled")
	}
	return &EmbedDetectResult{}
}

// DetectEmbedEndpoint is a convenience wrapper that returns only the endpoint.
// Use DetectOrLaunchEmbedServer when you need the server handle for cleanup.
func DetectEmbedEndpoint(logger *slog.Logger) *EmbedEndpoint {
	result := DetectOrLaunchEmbedServer(logger)
	return result.Endpoint
}

// getEmbedEnv reads DENEB_EMBED_URL and DENEB_EMBED_MODEL from environment.
func getEmbedEnv() (string, string) {
	return os.Getenv("DENEB_EMBED_URL"), os.Getenv("DENEB_EMBED_MODEL")
}

// probeEmbedModel calls /v1/models on the given base URL and returns
// the first model ID, or "" if the server is unreachable/empty.
func probeEmbedModel(baseURL string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}
	if len(result.Data) == 0 {
		return ""
	}
	return result.Data[0].ID
}
