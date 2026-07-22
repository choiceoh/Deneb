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
	pkgLocalAIHub   atomic.Pointer[localai.Hub]
)

// SetModelRoleRegistry sets the package-level model role registry.
// Called once during chat handler initialization.
func SetModelRoleRegistry(reg *modelrole.Registry) {
	if reg == nil {
		return
	}
	pkgRegistryOnce.Do(func() {
		pkgRegistry = reg
	})
}

// SetLocalAIHub sets the centralized local AI hub. When set, CallLocalLLM
// delegates to the hub instead of making direct calls.
func SetLocalAIHub(h *localai.Hub) {
	pkgLocalAIHub.Store(h)
}

// LocalAIHub returns the centralized local AI hub, or nil if not set.
// Used by callers (e.g., session memory) that need multi-message submission.
func LocalAIHub() *localai.Hub {
	return pkgLocalAIHub.Load()
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
	if hub := pkgLocalAIHub.Load(); hub != nil {
		return !hub.IsHealthy()
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
// wired; every other role (tiny, main) takes the direct path. Optional
// extraBody maps merge into the request body (e.g. chat_template_kwargs).
func CallRoleLLM(ctx context.Context, role modelrole.Role, system, userMessage string, maxTokens int, extraBody ...rawJSON) (string, error) {
	// Hub path: only the lightweight role is hub-managed today.
	if hub := pkgLocalAIHub.Load(); role == modelrole.RoleLightweight && hub != nil {
		return hub.CallLocalLLM(ctx, system, userMessage, maxTokens, extraBody...)
	}

	// Direct path: tiny/main, or lightweight before the hub is wired.
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := getRoleClient(role, modelrole.DefaultVllmBaseURL, "local")
	model := getRoleModel(role, modelrole.DefaultVllmModel)

	// Thinking-off shaping, shared with the localai hub (modelrole.
	// ThinkingOffDirectiveFor): the template toggle for dual-mode models
	// (deepseek-v4 → chat_template_kwargs.thinking=false), nothing for
	// untoggleable reasoning models (their thinking-only templates can 400
	// on enable_thinking), enable_thinking=false for vLLM-backed
	// non-reasoning models.
	// Registry-aware when possible so routing.toggleKwarg overrides apply;
	// the registry-less fallback assumes "vllm" — this direct path defaults
	// to DefaultVllmBaseURL anyway.
	providerID := "vllm"
	if pkgRegistry != nil {
		if p := pkgRegistry.Config(role).ProviderID; p != "" {
			providerID = p
		}
	}
	callerExtra := make(map[string]any)
	for _, body := range extraBody {
		if len(body) == 0 {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			return "", fmt.Errorf("decode pilot extra body: %w", err)
		}
		for key, value := range fields {
			callerExtra[key] = value
		}
	}
	// shapedExtra rebuilds the per-model request body: model-specific
	// thinking-off kwargs + caller extras + the server-side timeout. Computed
	// per attempt — the fallback chain crosses providers, and reusing the
	// primary model's kwargs would send e.g. a vLLM-only template toggle to a
	// cloud provider (or enable_thinking to an untoggleable reasoning model).
	shapedExtra := func(providerID, model string) map[string]any {
		// Role-aware: a speed/concurrency-first role (RoleTiny) forces thinking off
		// even when the per-model policy would leave it on, so the role's latency
		// stays low regardless of which model it points at.
		directive := pkgRegistry.ThinkingOffDirectiveForRole(role, providerID, model) // nil-receiver safe
		merged := make(map[string]any, len(callerExtra)+2)
		if directive != nil {
			merged["chat_template_kwargs"] = map[string]any{directive.TemplateKwarg(): false}
		}
		for k, v := range callerExtra {
			merged[k] = v
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
		return merged
	}

	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
		ExtraBody: pilotExtraBody(shapedExtra(providerID, model)),
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
				req.ExtraBody = pilotExtraBody(shapedExtra(fbCfg.ProviderID, fbCfg.Model))
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

// ReflectionDirective is an optional one-line self-check to append to the SYSTEM
// prompt of FREE-TEXT summary/analysis calls on the non-reasoning lightweight
// model. Non-reasoning models miss most of their own errors (arXiv:2507.02778);
// a reflective trigger sharply reduces that. Append ONLY at free-text sites —
// never on strict json_schema, single-token, or latency-critical (STW) calls,
// where a trailing instruction would corrupt the constrained output or blow a
// hard deadline.
const ReflectionDirective = "최종 출력 전에, 원문에 비추어 사실 오류나 빠뜨린 핵심이 없는지 한 번 더 점검한 뒤 최종 결과만 제시하라."

// CallLocalLLM invokes the lightweight model role — the original single local-AI
// tier, hub-managed when wired. Back-compat wrapper over CallRoleLLM.
func CallLocalLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...rawJSON) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleLightweight, system, userMessage, maxTokens, extraBody...)
}

// CallTinyLLM invokes the tiny model role — the smallest model, for trivial
// classification/extraction (session titles, gmail stage-1 extractors).
func CallTinyLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...rawJSON) (string, error) {
	return CallRoleLLM(ctx, modelrole.RoleTiny, system, userMessage, maxTokens, extraBody...)
}

// CollectStream reads all events from a streaming LLM response and returns the text.
func CollectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	if events == nil {
		return "", fmt.Errorf("nil event stream")
	}
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
				if text := ExtractDeltaText(ev.Payload.Bytes()); text != "" {
					sb.WriteString(text)
				}
			case "error":
				// Error events arrive in three shapes: the upstream raw
				// {"error":{"message"}} (openai.go passthrough) and the top-level
				// {"type":"error","message"} re-emitted by the OpenAI translator
				// and the SSE read-error event. The previous nested-only parse
				// silently swallowed the latter two — including stream-stall read
				// errors — returning partial text with a nil error. Mirror
				// mailanalysis's collectStreamText: try both shapes, fall back to the
				// raw payload, and always surface the error.
				var errPayload struct {
					Message string `json:"message"`
					Error   struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.Unmarshal(ev.Payload.Bytes(), &errPayload)
				msg := errPayload.Message
				if msg == "" {
					msg = errPayload.Error.Message
				}
				if msg == "" {
					msg = ev.Payload.String()
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
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	if maxChars < 0 {
		maxChars = 0
	}
	return string(runes[:maxChars]) + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}

func pilotExtraBody(m map[string]any) map[string]llm.FlexibleJSON {
	if m == nil {
		return nil
	}
	out := make(map[string]llm.FlexibleJSON, len(m))
	for k, v := range m {
		out[k] = llm.FlexibleFromValue(v)
	}
	return out
}
