package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
)

// SyncResult holds the outcome of a synchronous agent run.
type SyncResult struct {
	Text    string
	AllText string // accumulated text from all turns; used by cron delivery as a fallback when the final turn is NO_REPLY
	// DeliverableText is AllText minus the brief progress narration the model
	// emits alongside tool calls. Preferred by proactive/cron delivery so a
	// multi-turn report ships its answer turns without the "이제 위키 검색부터
	// 할게요" working narration. See agent.AgentResult.DeliverableText.
	DeliverableText string
	Model           string
	FellBack        bool // true when the model fallback chain fired (Model is the model that actually answered)
	InputTokens     int
	OutputTokens    int
	StopReason      string // "end_turn", "max_tokens", "tool_use", etc.
}

// BestText returns the answer to surface to the user. It prefers DeliverableText
// — the accumulation of every substantial answer turn with the interim
// "이제 ~할게요" tool-call narration removed — which fixes two failure modes at
// once: a short wrap-up final turn (the agent writes the body mid-run, then
// closes with "위키에 기록했습니다") no longer makes the answer vanish, and the
// working narration the model emits before tool calls never reaches the surface.
//
// Mirrors cronChatAdapter so every proactive surface agrees:
//   - DeliverableText present → use it (the common multi-turn case).
//   - else Text (the final turn) → use it.
//   - else AllText (last resort: a run that produced only narration before
//     aborting).
//
// NO_REPLY is stripped so the marker never leaks to the client.
func (r *SyncResult) BestText() string {
	if d := strings.TrimSpace(StripSilentToken(r.DeliverableText)); d != "" {
		return d
	}
	if t := strings.TrimSpace(r.Text); t != "" {
		return t
	}
	return strings.TrimSpace(StripSilentToken(r.AllText))
}

// SyncOptions holds optional parameters for synchronous agent runs.
// Used by the OpenAI-compatible HTTP endpoints to pass through sampling
// parameters and conversation context.
type SyncOptions struct {
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Stop             []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object

	// Messages provides a full conversation context (system, user, assistant,
	// tool messages). When set, this replaces the normal transcript-based
	// context assembly, and the `message` parameter is ignored.
	Messages []llm.Message

	// SystemPrompt provides a system prompt extracted from the messages array.
	// Used when Messages is set and system messages were present.
	SystemPrompt string

	// ToolPreset restricts available tools for this run (e.g. "boot", "conversation").
	// Empty means no restriction.
	ToolPreset string

	// MaxHistoryTokens overrides the transcript history token budget.
	// When set, assembleContext trims older messages to fit within this budget.
	MaxHistoryTokens int

	// Delivery carries channel routing for proactive tool sends (e.g. message.send).
	// Required in cron / scheduled contexts: without it the message tool fails
	// with "no active delivery target" and the agent falls back to text-only
	// replies that the cron delivery layer may not route correctly.
	Delivery *DeliveryContext

	// EphemeralUser suppresses persistence of the inbound user-role message —
	// see RunParams.EphemeralUser. Set by autonomous triggers (heartbeat) so
	// recurring self-triggers do not crowd out the recent-history window.
	EphemeralUser bool

	// EphemeralAssistant suppresses persistence of assistant/tool_result
	// messages produced during the run — see RunParams.EphemeralAssistant.
	// Heartbeat sets this true so autonomous ticks do not crowd out the
	// user's short-term conversation context; heartbeat state belongs in
	// HEARTBEAT.md instead.
	EphemeralAssistant bool

	// AutoDeliveredOutput marks a run whose final reply text is delivered by
	// the caller's run-completion path (e.g. the cron delivery layer) rather
	// than by the agent's in-loop `message` tool. Propagated to RunParams;
	// see RunParams.AutoDeliveredOutput.
	AutoDeliveredOutput bool
}

// prepareSyncRun builds RunParams and runDeps from the common sync arguments.
// Both SendSync and SendSyncStream share this setup.
func (h *Handler) prepareSyncRun(sessionKey, message, model, runIDPrefix string, opts *SyncOptions) (RunParams, runDeps, error) {
	if h.sessions == nil {
		return RunParams{}, runDeps{}, fmt.Errorf("chat handler not initialized")
	}

	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		sess = h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New(runIDPrefix),
	}

	if opts != nil {
		params.Temperature = opts.Temperature
		params.TopP = opts.TopP
		params.MaxTokens = opts.MaxTokens
		params.FrequencyPenalty = opts.FrequencyPenalty
		params.PresencePenalty = opts.PresencePenalty
		params.Stop = opts.Stop
		params.ResponseFormat = opts.ResponseFormat
		params.ToolChoice = opts.ToolChoice
		if len(opts.Messages) > 0 {
			params.PrebuiltMessages = opts.Messages
		}
		if opts.SystemPrompt != "" {
			params.System = opts.SystemPrompt
		}
		if opts.ToolPreset != "" {
			sess.ToolPreset = opts.ToolPreset
		}
		if opts.Delivery != nil {
			params.Delivery = opts.Delivery
		}
		params.EphemeralUser = opts.EphemeralUser
		params.EphemeralAssistant = opts.EphemeralAssistant
		params.AutoDeliveredOutput = opts.AutoDeliveredOutput
	}

	deps := h.buildRunDeps()
	if opts != nil && opts.MaxHistoryTokens > 0 {
		deps.contextCfg.MemoryTokenBudget = uint64(opts.MaxHistoryTokens)
	}

	return params, deps, nil
}

// buildSyncResult converts a chatRunResult into a SyncResult, resolving the
// model name through the fallback chain (explicit → default → registry).
func (h *Handler) buildSyncResult(model string, result *chatRunResult) (*SyncResult, error) {
	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.DefaultModel()
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	if result == nil {
		return nil, fmt.Errorf("agent run returned nil result")
	}

	// Prefer the model that actually answered (set when the fallback chain fired).
	if result.ActualModel != "" {
		resolvedModel = result.ActualModel
	}

	// Strip any chain-of-thought delimiters that leaked into the answer (see
	// reasoning_leak.go). The block regex matches here because the full assembled
	// text is available. TrimSpace cleans the gap a removed leading block leaves.
	return &SyncResult{
		Text:            strings.TrimSpace(stripReasoningLeak(result.Text)),
		AllText:         strings.TrimSpace(stripReasoningLeak(result.AllText)),
		DeliverableText: strings.TrimSpace(stripReasoningLeak(result.DeliverableText)),
		Model:           resolvedModel,
		FellBack:        result.FellBack,
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		StopReason:      result.StopReason,
	}, nil
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints that need the full response before replying.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string, opts *SyncOptions) (*SyncResult, error) {
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "sync", opts)
	if err != nil {
		return nil, err
	}

	result, err := executeAgentRun(ctx, params, deps, nil, nil, nil, h.logger, nil)
	if err != nil {
		return nil, err
	}
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}

// SendSyncStream runs the agent loop, calling onDelta for each text chunk,
// then returning the final result. Used by streaming OpenAI-compatible endpoints.
func (h *Handler) SendSyncStream(ctx context.Context, sessionKey, message, model string, opts *SyncOptions, onDelta func(string)) (*SyncResult, error) {
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "stream", opts)
	if err != nil {
		return nil, err
	}

	// Wrap onDelta to scrub leaked reasoning delimiters per chunk so a literal
	// "[thinking]" never reaches the stream. The block regex can't match across
	// delta boundaries, but the standalone-marker strip catches the tokens; the
	// final answer is fully cleaned in buildSyncResult. See reasoning_leak.go.
	streamDelta := onDelta
	if onDelta != nil {
		streamDelta = func(delta string) {
			if cleaned := stripReasoningLeak(delta); cleaned != "" {
				onDelta(cleaned)
			}
		}
	}

	result, err := executeAgentRunWithDelta(ctx, params, deps, streamDelta, h.logger)
	if err != nil {
		return nil, err
	}
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}
