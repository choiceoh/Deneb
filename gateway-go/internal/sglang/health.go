package sglang

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	healthCheckInterval = 30 * time.Second
	healthWarmupTTL     = 5 * time.Second
	healthWarmupPeriod  = 1 * time.Minute
	healthPingTimeout   = 3 * time.Second
	healthInferTimeout  = 5 * time.Second
	healthTestPrompt    = "1+1="
	healthTestMaxTokens = 4
)

// healthChecker runs periodic inference-based health probes.
type healthChecker struct {
	hub     *Hub
	baseURL string
	started time.Time
}

func (hc *healthChecker) interval() time.Duration {
	if time.Since(hc.started) < healthWarmupPeriod {
		return healthWarmupTTL
	}
	return healthCheckInterval
}

// run is the background health check loop.
func (hc *healthChecker) run() {
	defer hc.hub.wg.Done()

	// Immediate first check.
	hc.check()

	for {
		select {
		case <-hc.hub.ctx.Done():
			return
		case <-time.After(hc.interval()):
			hc.check()
		}
	}
}

func (hc *healthChecker) check() {
	// Phase 1: metadata ping (fast fail if server is completely down).
	if !hc.pingModels() {
		hc.hub.healthy.Store(false)
		hc.hub.lastHealthCheck.Store(time.Now().Unix())
		return
	}

	// Phase 2: actual inference test. This catches hung servers that still
	// respond to metadata endpoints.
	ctx, cancel := context.WithTimeout(hc.hub.ctx, healthInferTimeout)
	defer cancel()

	// Build a minimal request — 1 token input, 4 tokens output.
	req := llm.ChatRequest{
		Model:     hc.hub.model,
		Messages:  []llm.Message{llm.NewTextMessage("user", healthTestPrompt)},
		MaxTokens: healthTestMaxTokens,
		Stream:    true,
		ExtraBody: NoThinking,
	}

	events, err := hc.hub.client.StreamChat(ctx, req)
	if err != nil {
		hc.hub.healthy.Store(false)
		hc.hub.lastHealthCheck.Store(time.Now().Unix())
		hc.hub.logger.Debug("sglang health: inference failed", "error", err)
		return
	}

	// Drain the stream — we just need to confirm some tokens arrive.
	got := false
	for ev := range events {
		if ev.Type == "content_block_delta" {
			got = true
		}
	}

	hc.hub.healthy.Store(got)
	hc.hub.lastHealthCheck.Store(time.Now().Unix())
	if !got {
		hc.hub.logger.Debug("sglang health: inference returned no tokens")
	}
}

func (hc *healthChecker) pingModels() bool {
	ctx, cancel := context.WithTimeout(hc.hub.ctx, healthPingTimeout)
	defer cancel()

	url := hc.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// tryAbort sends a best-effort abort request to the sglang server.
// The /abort endpoint is sglang-specific and accepts {"rid": "..."}.
func tryAbort(baseURL, rid string) {
	if rid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	body := fmt.Sprintf(`{"rid":"%s"}`, rid)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/abort",
		bytes.NewReader([]byte(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
