package chat

import (
	"context"
	"fmt"

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

	result, err := executeAgentRun(ctx, params, deps, nil, nil, nil, h.logger, nil, nil)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.defaultModel
	}
	if resolvedModel == "" {
		resolvedModel = defaultModel
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

	// Create a custom broadcaster that calls the onDelta callback.
	// We reuse executeAgentRun's emitDelta path via a custom streamBroadcaster.
	var dummyBroadcast BroadcastRawFunc = func(_ string, _ []byte) int { return 0 }
	broadcaster := newStreamBroadcaster(dummyBroadcast, sessionKey, params.ClientRunID)

	// Override the emitDelta on the agent run by calling onDelta directly.
	// We achieve this by passing the onDelta via the broadcaster's EmitDelta,
	// but since executeAgentRun uses broadcaster.EmitDelta, we need a different approach.
	// Instead, we directly call executeAgentRun with a nil broadcaster and provide
	// emitDelta through the run deps. However, executeAgentRun doesn't support that.
	// The simplest approach: run executeAgentRun with our broadcaster and also
	// hook into the delta path.
	_ = broadcaster // unused in this approach

	// Direct approach: call the agent run inline, mimicking executeAgentRun
	// but with our delta callback.
	result, err := executeAgentRunWithDelta(ctx, params, deps, onDelta, h.logger)
	if err != nil {
		return nil, err
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.defaultModel
	}
	if resolvedModel == "" {
		resolvedModel = defaultModel
	}

	return &SyncResult{
		Text:         result.Text,
		Model:        resolvedModel,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}
