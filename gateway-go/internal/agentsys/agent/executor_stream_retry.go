// executor_stream_retry.go — one-turn stream connection and retry policy.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type streamRetryReason string

const (
	streamRetryNone  streamRetryReason = ""
	streamRetryIdle  streamRetryReason = "idle_timeout"
	streamRetryEvent streamRetryReason = "event_error"
)

type streamTerminationReason string

const (
	streamTerminationCompleted         streamTerminationReason = "completed"
	streamTerminationContextDone       streamTerminationReason = "context_done"
	streamTerminationInitialConnectErr streamTerminationReason = "initial_connection_error"
	streamTerminationRetryConnectErr   streamTerminationReason = "retry_connection_error"
	streamTerminationConsumeErr        streamTerminationReason = "stream_error"
	streamTerminationRetryBudgetSpent  streamTerminationReason = "retry_exhausted"
)

// streamingTurnOutcome carries both the retried turn's usable result and the
// attempt metadata RunAgent needs for stable error wrapping and run-level
// observability. A fresh turnResult is allocated before the retry so partial
// first-attempt output can still reach streaming hooks without leaking into
// the final assistant message.
type streamingTurnOutcome struct {
	result            *turnResult
	attempts          int
	retries           int
	retryReason       streamRetryReason
	terminationReason streamTerminationReason
}

func (o streamingTurnOutcome) initialConnectionFailed() bool {
	return o.terminationReason == streamTerminationInitialConnectErr
}

func (s *StreamStats) record(outcome streamingTurnOutcome) {
	s.Attempts += outcome.attempts
	s.Retries += outcome.retries
	if outcome.retryReason != streamRetryNone {
		s.LastRetryReason = string(outcome.retryReason)
	}
	s.TerminationReason = string(outcome.terminationReason)
}

// runStreamingTurnWithRetry owns the same-model stream retry policy for one
// agent turn. Initial connection failures are never retried. Once connected,
// an idle watchdog or provider error event gets exactly one retry; all other
// errors and context cancellation terminate immediately.
func runStreamingTurnWithRetry(
	ctx context.Context,
	client LLMStreamer,
	req llm.ChatRequest,
	hooks StreamHooks,
	idleTimeout time.Duration,
	logger *slog.Logger,
	turn int,
) (streamingTurnOutcome, error) {
	if logger == nil {
		logger = slog.Default()
	}
	outcome := streamingTurnOutcome{
		result:   &turnResult{},
		attempts: 1,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		outcome.terminationReason = streamTerminationInitialConnectErr
		if ctx.Err() != nil {
			outcome.terminationReason = streamTerminationContextDone
		}
		return outcome, err
	}

	err = consumeStreamInto(ctx, events, hooks, outcome.result, idleTimeout, logger)
	if err == nil {
		outcome.terminationReason = streamTerminationCompleted
		return outcome, nil
	}
	if ctx.Err() != nil {
		outcome.terminationReason = streamTerminationContextDone
		return outcome, err
	}

	outcome.retryReason = retryReasonForStreamError(err)
	if outcome.retryReason == streamRetryNone {
		outcome.terminationReason = streamTerminationConsumeErr
		return outcome, err
	}

	logger.Warn("stream interrupted, retrying turn on same model",
		"turn", turn,
		"error", err,
		"retryReason", outcome.retryReason,
		"idleTimeout", effectiveIdleTimeout(idleTimeout))

	outcome.retries = 1
	outcome.attempts++
	outcome.result = &turnResult{}
	events, err = client.StreamChat(ctx, req)
	if err != nil {
		outcome.terminationReason = streamTerminationRetryConnectErr
		if ctx.Err() != nil {
			outcome.terminationReason = streamTerminationContextDone
		}
		return outcome, err
	}

	err = consumeStreamInto(ctx, events, hooks, outcome.result, idleTimeout, logger)
	switch {
	case err == nil:
		outcome.terminationReason = streamTerminationCompleted
	case ctx.Err() != nil:
		outcome.terminationReason = streamTerminationContextDone
	default:
		outcome.terminationReason = streamTerminationRetryBudgetSpent
	}
	return outcome, err
}

func retryReasonForStreamError(err error) streamRetryReason {
	switch {
	case errors.Is(err, ErrStreamIdle):
		return streamRetryIdle
	case errors.Is(err, ErrStreamEvent):
		return streamRetryEvent
	default:
		return streamRetryNone
	}
}
