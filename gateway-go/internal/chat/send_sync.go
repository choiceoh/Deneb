package chat

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// SyncResult holds the outcome of a synchronous agent run.
type SyncResult struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
}

// StreamDelta is emitted for each text chunk during a streaming synchronous run.
type StreamDelta struct {
	Text string
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints that need the full response before replying.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string) (*SyncResult, error) {
	if h.sessions == nil {
		return nil, fmt.Errorf("chat handler not initialized")
	}

	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		sess = h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New("sync"),
	}

	deps := h.buildRunDeps()

	result, err := executeAgentRun(ctx, params, deps, nil, nil, nil, h.logger, nil)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.defaultModel
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	return &SyncResult{
		Text:         result.Text,
		Model:        resolvedModel,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}

// SendSyncStream runs the agent loop, calling onDelta for each text chunk,
// then returning the final result. Used by streaming OpenAI-compatible endpoints.
func (h *Handler) SendSyncStream(ctx context.Context, sessionKey, message, model string, onDelta func(string)) (*SyncResult, error) {
	if h.sessions == nil {
		return nil, fmt.Errorf("chat handler not initialized")
	}

	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		sess = h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New("stream"),
	}

	deps := h.buildRunDeps()

	result, err := executeAgentRunWithDelta(ctx, params, deps, onDelta, h.logger)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.defaultModel
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	return &SyncResult{
		Text:         result.Text,
		Model:        resolvedModel,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}
