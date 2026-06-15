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

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
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
// data list). The returned slice may be empty only when err != nil. An optional
// apiKey is sent as a Bearer token for endpoints that gate /models (e.g. the
// wormhole router); omit it for keyless local vLLM.
func DiscoverServedVllmModels(ctx context.Context, baseURL string, apiKey ...string) ([]string, error) {
	infos, err := DiscoverServedVllmModelInfos(ctx, baseURL, apiKey...)
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
func DiscoverServedVllmModelInfos(ctx context.Context, baseURL string, apiKey ...string) ([]ServedModelInfo, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if len(apiKey) > 0 && apiKey[0] != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey[0])
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
// vLLM server is actually serving, and returns the discovery payload (nil
// when the probe was skipped or failed) so callers can also harvest the
// advertised context lengths. Behaviour:
//
//   - Provider != vllm: no-op (other providers have their own discovery
//     conventions and we don't want to silently substitute against them).
//   - Configured model is in the served list: name untouched (operator's
//     intent respected).
//   - Configured model not in served list: replace with the first served
//     id and log INFO so the substitution is visible.
//   - Probe fails (server down, bad payload): no-op + WARN.
//
// The chat pipeline already retries through the fallback chain, so a
// missing-model 404 isn't catastrophic; this just removes the most common
// "I renamed the served model" footgun.
// harvestVllmWindows probes configured vLLM-backed providers for context windows
// even when no role routes through them, folding each served model's
// max_model_len into `into` (keyed by served model id, never overwriting a window
// a role probe already found). This is what lets a model fronted by wormhole
// still resolve its real window: wormhole's /v1/models reports each local model's
// max_model_len, and the still-configured direct vllm provider serves the same id
// as a fallback. Probing both vllm AND wormhole (modelcaps.ServesVllmBacked) means
// the window survives even if one source is removed. Best-effort: a down or
// non-reporting provider (e.g. a cloud model fronted by wormhole, whose row omits
// the field) contributes nothing. `probed` carries the baseURLs the per-role
// reconcile already hit, so a provider backing a vLLM role is not probed twice.
func harvestVllmWindows(logger *slog.Logger, providers map[string]ProviderResolved, into map[string]int, probed map[string]bool) {
	for id, pr := range providers {
		if pr.BaseURL == "" || !modelcaps.ServesVllmBacked(id) || probed[pr.BaseURL] {
			continue
		}
		probed[pr.BaseURL] = true
		ctx, cancel := context.WithTimeout(context.Background(), vllmDiscoveryTimeout)
		infos, err := DiscoverServedVllmModelInfos(ctx, pr.BaseURL, pr.APIKey)
		cancel()
		if err != nil {
			logger.Warn("modelrole: vllm window probe failed (unused provider)",
				"provider", id, "baseUrl", pr.BaseURL, "error", err)
			continue
		}
		for _, info := range infos {
			if info.MaxModelLen > 0 {
				if _, ok := into[info.ID]; !ok {
					into[info.ID] = info.MaxModelLen
				}
			}
		}
	}
}

func reconcileVllmModel(logger *slog.Logger, cfg *ModelConfig) []ServedModelInfo {
	if cfg == nil || cfg.ProviderID != "vllm" || cfg.BaseURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), vllmDiscoveryTimeout)
	defer cancel()
	served, err := DiscoverServedVllmModelInfos(ctx, cfg.BaseURL)
	if err != nil {
		logger.Warn("modelrole: vllm probe failed; using configured name as-is",
			"configured", cfg.Model, "baseUrl", cfg.BaseURL, "error", err)
		return nil
	}
	for _, info := range served {
		if info.ID == cfg.Model {
			return served // configured name matches what's served
		}
	}
	prev := cfg.Model
	cfg.Model = served[0].ID
	if prev == "" {
		logger.Info("modelrole: vllm model auto-discovered",
			"served", cfg.Model, "baseUrl", cfg.BaseURL)
	} else {
		ids := make([]string, 0, len(served))
		for _, info := range served {
			ids = append(ids, info.ID)
		}
		logger.Warn("modelrole: vllm configured model not served; using first served instead",
			"configured", prev, "served", cfg.Model, "available", ids, "baseUrl", cfg.BaseURL)
	}
	return served
}
