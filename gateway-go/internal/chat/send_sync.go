package chat

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// SyncResult holds the outcome of a synchronous agent run.
type SyncResult struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
	StopReason   string // "end_turn", "max_tokens", "tool_use", etc.
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
}

// StreamDelta is emitted for each text chunk during a streaming synchronous run.
type StreamDelta struct {
	Text string
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints that need the full response before replying.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string, opts *SyncOptions) (*SyncResult, error) {
	if h.sessions == nil {
		return nil, fmt.Errorf("chat handler not initialized")
	}

	if h.sessions.Get(sessionKey) == nil {
		h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New("sync"),
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
	}

	deps := h.buildRunDeps()
	deps.continuationEnabled = false // sync paths do not support autonomous continuation

	result, err := executeAgentRun(ctx, params, deps, nil, nil, nil, h.logger, nil)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.DefaultModel()
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	stopReason := ""
	if result != nil {
		stopReason = result.StopReason
	}

	return &SyncResult{
		Text:         result.Text,
		Model:        resolvedModel,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		StopReason:   stopReason,
	}, nil
}

// SendSyncStream runs the agent loop, calling onDelta for each text chunk,
// then returning the final result. Used by streaming OpenAI-compatible endpoints.
func (h *Handler) SendSyncStream(ctx context.Context, sessionKey, message, model string, opts *SyncOptions, onDelta func(string)) (*SyncResult, error) {
	if h.sessions == nil {
		return nil, fmt.Errorf("chat handler not initialized")
	}

	if h.sessions.Get(sessionKey) == nil {
		h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New("stream"),
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
	}

	deps := h.buildRunDeps()
	deps.continuationEnabled = false // sync paths do not support autonomous continuation

	result, err := executeAgentRunWithDelta(ctx, params, deps, onDelta, h.logger)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.DefaultModel()
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	stopReason := ""
	if result != nil {
		stopReason = result.StopReason
	}

	return &SyncResult{
		Text:         result.Text,
		Model:        resolvedModel,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		StopReason:   stopReason,
	}, nil
}
