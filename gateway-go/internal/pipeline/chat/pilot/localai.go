package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// --- Package-level model role registry ---
// Set once during handler initialization via SetModelRoleRegistry.
// Used by role-based LLM helpers, CheckLocalAIHealth, and other lightweight-model code.

var (
	pkgRegistry     *modelrole.Registry
	pkgRegistryOnce sync.Once
	pkgLocalAIHub   *localai.Hub
)

// SetModelRoleRegistry sets the package-level model role registry.
// Called once during chat handler initialization.
func SetModelRoleRegistry(reg *modelrole.Registry) {
	pkgRegistryOnce.Do(func() {
		pkgRegistry = reg
	})
}

// SetLocalAIHub sets the centralized local AI hub. When set, CallLocalLLM and
// CheckLocalAIHealth delegate to the hub instead of making direct calls.
func SetLocalAIHub(h *localai.Hub) {
	pkgLocalAIHub = h
}

// LocalAIHub returns the centralized local AI hub, or nil if not set.
// Used by callers (e.g., session memory) that need multi-message submission.
func LocalAIHub() *localai.Hub {
	return pkgLocalAIHub
}

// LightweightBaseURL returns the base URL for the lightweight model.
func LightweightBaseURL() string {
	if pkgRegistry != nil {
		return pkgRegistry.BaseURL(modelrole.RoleLightweight)
	}
	return modelrole.DefaultVllmBaseURL
}

// --- local AI health check (cached) ---

const (
	localAIHealthTTL  = 30 * time.Second
	localAIWarmupTTL  = 5 * time.Second
	localAIWarmupFor  = 2 * time.Minute
	localAIHealthPing = 3 * time.Second
	pilotTimeout      = 2 * time.Minute
)

var (
	localAIHealthy   atomic.Bool
	localAILastCheck atomic.Int64 // unix timestamp
	localAIStartedAt = time.Now()
)

// LocalAIRecentlyDown returns true if local AI is known to be unhealthy.
// When the hub is set, delegates to the hub's cached health state (background
// inference-based probe). Otherwise falls back to the legacy atomic cache.
func LocalAIRecentlyDown() bool {
	if pkgLocalAIHub != nil {
		return !pkgLocalAIHub.IsHealthy()
	}
	return !localAIHealthy.Load() && localAILastCheck.Load() > 0
}

// HasRegistry returns true if the model role registry has been set.
func HasRegistry() bool {
	return pkgRegistry != nil
}

// CheckLocalAIHealth returns true if the local AI server is reachable.
// When the local AI hub is set, delegates to the hub's inference-based health check.
// Otherwise falls back to the legacy /v1/models metadata probe.
func CheckLocalAIHealth() bool {
	if pkgLocalAIHub != nil {
		return pkgLocalAIHub.IsHealthy()
	}

	// Legacy fallback: metadata-only probe.
	now := time.Now().Unix()
	last := localAILastCheck.Load()
	ttl := localAIHealthTTL
	if time.Since(localAIStartedAt) < localAIWarmupFor {
		ttl = localAIWarmupTTL
	}
	if now-last < int64(ttl.Seconds()) {
		return localAIHealthy.Load()
	}

	ctx, cancel := context.WithTimeout(context.Background(), localAIHealthPing)
	defer cancel()

	baseURL := LightweightBaseURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		localAIHealthy.Store(false)
		localAILastCheck.Store(now)
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		localAIHealthy.Store(false)
		localAILastCheck.Store(now)
		return false
	}
	resp.Body.Close()

	healthy := resp.StatusCode == http.StatusOK
	localAIHealthy.Store(healthy)
	localAILastCheck.Store(now)
	return healthy
}

// --- Helpers ---

func getRoleClient(role modelrole.Role, defaultBaseURL, defaultAPIKey string) *llm.Client {
	if pkgRegistry != nil {
		return pkgRegistry.Client(role)
	}
	return llm.NewClient(defaultBaseURL, defaultAPIKey, llm.WithLogger(slog.Default()))
}

func getRoleModel(role modelrole.Role, defaultModel string) string {
	if pkgRegistry != nil {
		return pkgRegistry.Model(role)
	}
	return defaultModel
}

// LightweightClient returns the cached LLM client for the lightweight model role.
func LightweightClient() *llm.Client {
	return getRoleClient(modelrole.RoleLightweight, modelrole.DefaultVllmBaseURL, "local")
}

// LightweightModel returns the model name for the lightweight role.
func LightweightModel() string {
	return getRoleModel(modelrole.RoleLightweight, modelrole.DefaultVllmModel)
}

// CallLocalLLM invokes the lightweight (local AI) model with fallback chain.
// When the local AI hub is set, delegates to the hub for token budget management,
// priority queuing, and zombie request prevention.
// Optional extraBody maps are merged into the request body (e.g. for chat_template_kwargs).
// Reasoning mode is disabled by default for all calls.
func CallLocalLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	// Hub path: centralized token budget, priority queue, health check.
	if pkgLocalAIHub != nil {
		return pkgLocalAIHub.CallLocalLLM(ctx, system, userMessage, maxTokens, extraBody...)
	}

	// Legacy path: direct local AI call (used when hub is not yet wired).
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := LightweightClient()
	model := LightweightModel()

	// Always disable reasoning by default; caller-supplied extraBody merges on top.
	merged := make(map[string]any, len(localai.NoThinking))
	for k, v := range localai.NoThinking {
		merged[k] = v
	}
	if len(extraBody) > 0 && extraBody[0] != nil {
		for k, v := range extraBody[0] {
			merged[k] = v
		}
	}

	// Inject server-side timeout so local AI aborts generation when the
	// gateway's context deadline expires. Without this, cancelled requests
	// become zombies that hold KV cache until max_tokens is exhausted.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline).Seconds() - 2.0 // 2s headroom for network
		if remaining > 1 {
			merged["timeout"] = remaining
		}
	}

	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
		ExtraBody: merged,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		// Lightweight model failed — try fallback model if registry is available.
		if pkgRegistry != nil {
			fbChain := pkgRegistry.FallbackChain(modelrole.RoleLightweight)
			for _, role := range fbChain[1:] {
				fbCfg := pkgRegistry.Config(role)
				fbClient := pkgRegistry.Client(role)
				if fbClient == nil {
					continue
				}
				req.Model = fbCfg.Model
				events, err = fbClient.StreamChat(ctx, req)
				if err == nil {
					break
				}
			}
			if err != nil {
				return "", fmt.Errorf("all models failed: %w", err)
			}
		} else {
			return "", fmt.Errorf("localai stream: %w", err)
		}
	}

	text, err := CollectStream(ctx, events)
	if err != nil {
		return "", err
	}

	if text == "" {
		return "(no response from local model)", nil
	}
	return text, nil
}

// CollectStream reads all events from a streaming LLM response and returns the text.
func CollectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return sb.String(), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return sb.String(), nil
			}
			switch ev.Type {
			case "content_block_delta":
				if text := ExtractDeltaText(ev.Payload); text != "" {
					sb.WriteString(text)
				}
			case "error":
				var errPayload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal(ev.Payload, &errPayload) == nil && errPayload.Error.Message != "" {
					return sb.String(), fmt.Errorf("stream error: %s", errPayload.Error.Message)
				}
			}
		}
	}
}

// ExtractDeltaText extracts the "text" field from {"delta":{"text":"..."}} payloads
// by scanning the raw bytes directly, avoiding the string(payload) allocation on
// every streaming delta event. Falls back to json.Unmarshal only when backslash
// escapes are detected (rare).
func ExtractDeltaText(payload []byte) string {
	marker := []byte(`"text":"`)
	idx := bytes.Index(payload, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	for i := start; i < len(payload); i++ {
		switch payload[i] {
		case '"':
			return string(payload[start:i])
		case '\\':
			// Escape sequence present — fall back to json.Unmarshal for correctness.
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(payload, &delta) == nil {
				return delta.Delta.Text
			}
			return ""
		}
	}
	return ""
}

// TruncateInput is a simple head-only truncation.
func TruncateInput(s string, maxChars int) string {
	return TruncateHead(s, maxChars)
}

// TruncateHead is a simple head-only truncation (used for chain prompts, fallback).
func TruncateHead(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}
