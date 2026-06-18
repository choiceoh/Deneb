package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
// Used by role-based LLM helpers and other lightweight-model code.

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

// SetLocalAIHub sets the centralized local AI hub. When set, CallLocalLLM
// delegates to the hub instead of making direct calls.
func SetLocalAIHub(h *localai.Hub) {
	pkgLocalAIHub = h
}

// LocalAIHub returns the centralized local AI hub, or nil if not set.
// Used by callers (e.g., session memory) that need multi-message submission.
func LocalAIHub() *localai.Hub {
	return pkgLocalAIHub
}

// --- local AI health check (cached) ---

const (
	pilotTimeout = 2 * time.Minute
)

var (
	localAIHealthy   atomic.Bool
	localAILastCheck atomic.Int64 // unix timestamp
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

// LightweightModel returns the model name for the lightweight role.
func LightweightModel() string {
	return getRoleModel(modelrole.RoleLightweight, modelrole.DefaultVllmModel)
}

// CallRoleLLM invokes a specific model role with reasoning-aware request shaping
// and the role's fallback chain. The lightweight role routes through the
// centralized hub (token budget, priority queue, zombie guard) when one is
// wired; every other role (tiny, analysis) takes the direct path. Optional
// extraBody maps merge into the request body (e.g. chat_template_kwargs).
func CallRoleLLM(ctx context.Context, role modelrole.Role, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	// Hub path: only the lightweight role is hub-managed today.
	if role == modelrole.RoleLightweight && pkgLocalAIHub != nil {
		return pkgLocalAIHub.CallLocalLLM(ctx, system, userMessage, maxTokens, extraBody...)
	}

	// Direct path: tiny/analysis, or lightweight before the hub is wired.
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := getRoleClient(role, modelrole.DefaultVllmBaseURL, "local")
	model := getRoleModel(role, modelrole.DefaultVllmModel)

	// Disable reasoning only for non-reasoning models — a reasoning model's
	// thinking-only chat template can 400 on enable_thinking (mirrors
	// localai.Hub.mergeRequestBody). Caller-supplied extraBody merges on top.
	merged := make(map[string]any, len(localai.NoThinking)+1)
	if !modelrole.IsReasoningModel(model) {
		for k, v := range localai.NoThinking {
			merged[k] = v
		}
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
		// Role model failed — walk its fallback chain if the registry is available.
		if pkgRegistry != nil {
			fbChain := pkgRegistry.FallbackChain(role)
			for _, fbRole := range fbChain[1:] {
				fbCfg := pkgRegistry.Config(fbRole)
				fbClient := pkgRegistry.Client(fbRole)
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

// CallLocalLLM invokes the lightweight model role — the original single local-AI
// tier, hub-managed when wired. Back-compat wrapper over CallRoleLLM.
func CallLocalLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleLightweight, system, userMessage, maxTokens, extraBody...)
}

// CallTinyLLM invokes the tiny model role — the smallest model, for trivial
// classification/extraction (session titles, gmail stage-1 extractors).
func CallTinyLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleTiny, system, userMessage, maxTokens, extraBody...)
}

// CallAnalysisLLM invokes the analysis model role — the highest-quality local
// model, for reasoning-grade tasks (gmail stage-2 analysis, conversation
// compaction, transcript summary).
func CallAnalysisLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleAnalysis, system, userMessage, maxTokens, extraBody...)
}

// CallCodingLLM invokes the coding model role for code-writing/editing tasks.
// The role is opt-in; callers that require a configured coding role should
// check the registry before calling.
func CallCodingLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleCoding, system, userMessage, maxTokens, extraBody...)
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
				// Error events arrive in three shapes: the upstream raw
				// {"error":{"message"}} (openai.go passthrough) and the top-level
				// {"type":"error","message"} re-emitted by the OpenAI translator
				// and the SSE read-error event. The previous nested-only parse
				// silently swallowed the latter two — including stream-stall read
				// errors — returning partial text with a nil error. Mirror
				// gmailpoll's collectStreamText: try both shapes, fall back to the
				// raw payload, and always surface the error.
				var errPayload struct {
					Message string `json:"message"`
					Error   struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.Unmarshal(ev.Payload, &errPayload)
				msg := errPayload.Message
				if msg == "" {
					msg = errPayload.Error.Message
				}
				if msg == "" {
					msg = string(ev.Payload)
				}
				return sb.String(), fmt.Errorf("stream error: %s", msg)
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

// TruncateHead is a simple head-only truncation (used for chain prompts, fallback).
func TruncateHead(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}
