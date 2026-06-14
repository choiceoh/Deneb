package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fleetSource points wormhole at a SparkFleet control-plane for model discovery.
// SparkFleet (the GB10 fleet manager) knows which local vLLM backends are up and
// what model each serves; wormhole reads that inventory so launching a model in
// SparkFleet makes it routable here without hand-editing the config. This is a
// loose, one-way coupling: wormhole (the data plane — model access) reads
// SparkFleet (the control plane — model lifecycle), never the reverse. Token
// supports ${ENV} expansion (done in loadConfig), matching model keys.
type fleetSource struct {
	URL   string `json:"url"`             // SparkFleet base, e.g. http://127.0.0.1:18900
	Token string `json:"token,omitempty"` // sent as X-Fleet-Token when SparkFleet requires auth
}

// fleetServiceView is the subset of SparkFleet's GET /api/services record that
// wormhole needs. SparkFleet sets Model only for endpoints that advertise a
// served model id at /v1/models — i.e. exactly the OpenAI-compatible vLLM chat
// backends — so a non-empty Model is our filter for "routable chat backend".
type fleetServiceView struct {
	Node          string `json:"node"`
	Name          string `json:"name"`
	URL           string `json:"url"` // SparkFleet's probe URL, e.g. http://127.0.0.1:8000/v1/models
	OK            bool   `json:"ok"`
	Model         string `json:"model"`
	NodeReachable bool   `json:"nodeReachable"`
}

// discoverFleet asks SparkFleet which local vLLM backends are live and turns each
// healthy one into a routable modelEntry: name = the served model id, url = its
// OpenAI base, protocol = openai, local = true, and NO key (on-box GPU backends
// need no auth). Down or non-chat services (empty Model) are skipped. If two
// nodes serve the same model id, the first wins (dedup by name).
func discoverFleet(ctx context.Context, client *http.Client, src fleetSource) ([]modelEntry, error) {
	url := strings.TrimRight(src.URL, "/") + "/api/services"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if src.Token != "" {
		req.Header.Set("X-Fleet-Token", src.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sparkfleet /api/services: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Services []fleetServiceView `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode sparkfleet services: %w", err)
	}

	out := make([]modelEntry, 0, len(payload.Services))
	seen := make(map[string]bool, len(payload.Services))
	for _, sv := range payload.Services {
		if !sv.OK || sv.Model == "" {
			continue // down, or not an OpenAI vLLM chat backend
		}
		base := deriveOpenAIBase(sv.URL)
		if base == "" {
			continue // probe URL isn't an OpenAI /v1 endpoint we can forward to
		}
		if seen[sv.Model] {
			continue // first node serving this model id wins
		}
		seen[sv.Model] = true
		local := true
		out = append(out, modelEntry{
			Name:          sv.Model,
			URL:           base,
			UpstreamModel: sv.Model,
			Protocol:      protocolOpenAI,
			Local:         &local,
		})
	}
	return out, nil
}

// deriveOpenAIBase turns a SparkFleet probe URL into the OpenAI base wormhole
// forwards to. SparkFleet probes a vLLM at its /v1/models; doUpstream appends
// /chat/completions to the base, so we want the .../v1 prefix. Anything that
// isn't a vLLM /v1 endpoint returns "" (skipped) — defensive against a /health
// probe URL sneaking through with a model id set.
func deriveOpenAIBase(probeURL string) string {
	u := strings.TrimRight(strings.TrimSpace(probeURL), "/")
	switch {
	case strings.HasSuffix(u, "/v1/models"):
		return strings.TrimSuffix(u, "/models")
	case strings.HasSuffix(u, "/v1"):
		return u
	default:
		return ""
	}
}
