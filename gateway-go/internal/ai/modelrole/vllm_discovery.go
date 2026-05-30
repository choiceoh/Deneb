package modelrole

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// vllmDiscoveryTimeout caps the auto-discovery probe so a slow or unreachable
// vLLM server cannot stall gateway startup.
const vllmDiscoveryTimeout = 3 * time.Second

// vllmDiscoveryClient is the HTTP client used by DiscoverServedVllmModels.
// Overridden in tests to point at httptest servers.
var vllmDiscoveryClient = &http.Client{Timeout: vllmDiscoveryTimeout}

// ServedModelInfo describes one model reported by an OpenAI-compatible
// /models endpoint, including its advertised context length when present.
type ServedModelInfo struct {
	ID          string
	MaxModelLen int // context length in tokens; 0 when the server omits it
}

// DiscoverServedVllmModels probes an OpenAI-compatible /models endpoint and
// returns the served model ids in the order the server reports them.
// Returns a non-nil error when the probe fails (network, bad payload, empty
// data list). The returned slice may be empty only when err != nil.
func DiscoverServedVllmModels(ctx context.Context, baseURL string) ([]string, error) {
	infos, err := DiscoverServedVllmModelInfos(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(infos))
	for _, m := range infos {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// DiscoverServedVllmModelInfos is like DiscoverServedVllmModels but also
// reports each model's advertised context length (max_model_len). Used to
// auto-populate a custom model's contextWindow when it is added, so the
// persisted entry is complete rather than a bare {"id": ...} stub.
func DiscoverServedVllmModelInfos(ctx context.Context, baseURL string) ([]ServedModelInfo, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := vllmDiscoveryClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 {
		return nil, errors.New("no models served")
	}
	infos := make([]ServedModelInfo, 0, len(payload.Data))
	for _, m := range payload.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			infos = append(infos, ServedModelInfo{ID: id, MaxModelLen: m.MaxModelLen})
		}
	}
	if len(infos) == 0 {
		return nil, errors.New("no model ids in response")
	}
	return infos, nil
}

// reconcileVllmModel rewrites cfg.Model so it matches whatever the local
// vLLM server is actually serving. Behaviour:
//
//   - Provider != vllm: no-op (other providers have their own discovery
//     conventions and we don't want to silently substitute against them).
//   - Configured model is in the served list: no-op (operator's intent
//     respected).
//   - Configured model not in served list: replace with the first served
//     id and log INFO so the substitution is visible.
//   - Probe fails (server down, bad payload): no-op + WARN.
//
// The chat pipeline already retries through the fallback chain, so a
// missing-model 404 isn't catastrophic; this just removes the most common
// "I renamed the served model" footgun.
func reconcileVllmModel(logger *slog.Logger, cfg *ModelConfig) {
	if cfg == nil || cfg.ProviderID != "vllm" || cfg.BaseURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), vllmDiscoveryTimeout)
	defer cancel()
	served, err := DiscoverServedVllmModels(ctx, cfg.BaseURL)
	if err != nil {
		logger.Warn("modelrole: vllm probe failed; using configured name as-is",
			"configured", cfg.Model, "baseUrl", cfg.BaseURL, "error", err)
		return
	}
	for _, id := range served {
		if id == cfg.Model {
			return // configured name matches what's served
		}
	}
	prev := cfg.Model
	cfg.Model = served[0]
	if prev == "" {
		logger.Info("modelrole: vllm model auto-discovered",
			"served", cfg.Model, "baseUrl", cfg.BaseURL)
	} else {
		logger.Warn("modelrole: vllm configured model not served; using first served instead",
			"configured", prev, "served", cfg.Model, "available", served, "baseUrl", cfg.BaseURL)
	}
}
