package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// ErrRuntimeDraining tells callers to retry after the gateway restart. It is
// distinct from context cancellation: turns admitted before draining continue
// to completion, while only new work receives this error.
var ErrRuntimeDraining = errors.New("chat runtime is draining for restart")

var (
	_ chatport.SyncRunner       = (*Handler)(nil)
	_ chatport.SyncStreamRunner = (*Handler)(nil)
	_ chatport.ModelController  = (*Handler)(nil)
)

// ChatReady reports whether the concrete handler behind a chatport interface
// is currently admitting new runs. It is deliberately safe on a nil receiver
// so consumers can reject a typed nil pointer stored in an interface before
// dispatching a run.
func (h *Handler) ChatReady() bool {
	return h != nil && h.abort != nil && !h.abort.IsDraining()
}

// BeginDrain closes admission and waits for all already-accepted runs to leave
// the abort registry. The caller controls the maximum wait with ctx; timing out
// does not reopen admission.
func (h *Handler) BeginDrain(ctx context.Context) error {
	if h == nil || h.abort == nil {
		return nil
	}
	select {
	case <-h.abort.BeginDrain():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunSync executes the runtime-safe chatport request through the richer chat
// implementation API.
func (h *Handler) RunSync(ctx context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	if h == nil {
		return nil, fmt.Errorf("chat handler is not ready")
	}
	result, err := h.SendSync(ctx, req.SessionKey, req.Message, req.Model, syncOptionsFromPort(req))
	if err != nil {
		return nil, err
	}
	return syncResultToPort(result), nil
}

// RunSyncStream is RunSync with direct text-delta delivery.
func (h *Handler) RunSyncStream(
	ctx context.Context,
	req chatport.SyncRequest,
	onDelta func(string),
) (*chatport.SyncResult, error) {
	if h == nil {
		return nil, fmt.Errorf("chat handler is not ready")
	}
	result, err := h.SendSyncStream(
		ctx,
		req.SessionKey,
		req.Message,
		req.Model,
		syncOptionsFromPort(req),
		onDelta,
	)
	if err != nil {
		return nil, err
	}
	return syncResultToPort(result), nil
}

func syncOptionsFromPort(req chatport.SyncRequest) *SyncOptions {
	options := &SyncOptions{
		MaxTokens:           req.MaxTokens,
		MaxTurns:            req.MaxTurns,
		MaxToolCallAttempts: req.MaxToolCallAttempts,
		SystemPrompt:        req.SystemPrompt,
		Thinking:            req.Thinking,
		ToolPreset:          req.ToolPreset,
		MaxHistoryTokens:    req.MaxHistoryTokens,
		Delivery:            req.Delivery,
		EphemeralUser:       req.EphemeralUser,
		EphemeralAssistant:  req.EphemeralAssistant,
		AutoDeliveredOutput: req.AutoDeliveredOutput,
		SkipRecall:          req.SkipRecall,
		FeedContext:         req.FeedContext,
		GateUntrustedTools:  req.GateUntrustedTools,
		BeforeToolCall:      req.BeforeToolCall,
		OnToolResult:        req.OnToolResult,
		OnThinking:          req.OnThinking,
		OnReasoning:         req.OnReasoning,
	}
	if req.OnToolEvent != nil {
		options.OnToolEvent = func(event ToolStreamEvent) {
			req.OnToolEvent(chatport.ToolStreamEvent{
				State:     event.State,
				Tool:      event.Tool,
				ToolUseID: event.ToolUseID,
				Detail:    event.Detail,
				IsError:   event.IsError,
			})
		}
	}
	return options
}

func syncResultToPort(result *SyncResult) *chatport.SyncResult {
	if result == nil {
		return nil
	}
	return &chatport.SyncResult{
		Text:            result.Text,
		AllText:         result.AllText,
		DeliverableText: result.DeliverableText,
		BestText:        result.BestText(),
		Model:           result.Model,
		ProviderModel:   result.ProviderModel,
		FellBack:        result.FellBack,
		InputTokens:     result.InputTokens,
		OutputTokens:    result.OutputTokens,
		Turns:           result.Turns,
		StopReason:      result.StopReason,
		Thinking:        result.Thinking,
	}
}
