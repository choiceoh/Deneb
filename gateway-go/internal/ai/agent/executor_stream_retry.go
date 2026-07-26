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
	streamTerminationPreOutputIdle     streamTerminationReason = "pre_output_idle"
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
	streamBytes       int
}

func (o streamingTurnOutcome) initialConnectionFailed() bool {
	return o.terminationReason == streamTerminationInitialConnectErr
}

func (o streamingTurnOutcome) preOutputIdle() bool {
	return o.terminationReason == streamTerminationPreOutputIdle
}

func (r *turnResult) assistantOutputStarted() bool {
	return r != nil && (r.contentStarted || r.text != "" || len(r.toolCalls) > 0 || len(r.contentBlocks) > 0)
}

func (s *StreamStats) record(outcome streamingTurnOutcome) {
	s.Attempts += outcome.attempts
	s.Retries += outcome.retries
	if outcome.retryReason != streamRetryNone {
		s.LastRetryReason = string(outcome.retryReason)
	}
	s.TerminationReason = string(outcome.terminationReason)
}

// runStreamingTurnWithPolicy owns the same-model stream retry policy for one
// agent turn, including deterministic-run retry toggles and translated stream
// limits. Initial connection failures are never retried. Once connected, an
// idle watchdog or provider error event gets exactly one retry; all other
// errors and context cancellation terminate immediately.
func runStreamingTurnWithPolicy(
	ctx context.Context,
	client LLMStreamer,
	req llm.ChatRequest,
	hooks StreamHooks,
	idleTimeout time.Duration,
	logger *slog.Logger,
	turn int,
	disableRetry bool,
	maxStreamBytes int,
) (streamingTurnOutcome, error) {
	if logger == nil {
		logger = slog.Default()
	}
	outcome := streamingTurnOutcome{
		attempts: 1,
	}

	result, connected, err := runStreamingAttemptWithLimit(ctx, client, req, hooks, idleTimeout, logger, maxStreamBytes)
	outcome.result = result
	outcome.streamBytes += result.streamBytes
	if !connected {
		outcome.terminationReason = streamTerminationInitialConnectErr
		if ctx.Err() != nil {
			outcome.terminationReason = streamTerminationContextDone
		}
		return outcome, err
	}

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
	if outcome.retryReason == streamRetryIdle && !result.assistantOutputStarted() {
		outcome.terminationReason = streamTerminationPreOutputIdle
		return outcome, err
	}
	if disableRetry {
		outcome.terminationReason = streamTerminationConsumeErr
		return outcome, err
	}

	retryStreamBytes := maxStreamBytes
	if maxStreamBytes > 0 {
		retryStreamBytes -= outcome.streamBytes
		if retryStreamBytes <= 0 {
			outcome.terminationReason = streamTerminationRetryBudgetSpent
			return outcome, ErrStreamLimit
		}
	}

	logger.Warn("stream interrupted, retrying turn on same model",
		"turn", turn,
		"error", err,
		"retryReason", outcome.retryReason,
		"idleTimeout", effectiveIdleTimeout(idleTimeout))

	outcome.retries = 1
	outcome.attempts++
	result, connected, err = runStreamingAttemptWithLimit(ctx, client, req, hooks, idleTimeout, logger, retryStreamBytes)
	outcome.result = result
	outcome.streamBytes += result.streamBytes
	if !connected {
		outcome.terminationReason = streamTerminationRetryConnectErr
		if ctx.Err() != nil {
			outcome.terminationReason = streamTerminationContextDone
		}
		return outcome, err
	}

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

// runStreamingAttemptWithLimit gives every connection its own cancellation
// boundary. consumeStreamInto may abandon a still-open provider stream after an
// idle timeout or error event; canceling here releases that attempt's response
// body, parser, and forwarder before the caller starts a retry.
func runStreamingAttemptWithLimit(
	ctx context.Context,
	client LLMStreamer,
	req llm.ChatRequest,
	hooks StreamHooks,
	idleTimeout time.Duration,
	logger *slog.Logger,
	maxStreamBytes int,
) (*turnResult, bool, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	result := &turnResult{maxStreamBytes: maxStreamBytes}
	events, err := client.StreamChat(attemptCtx, req)
	if err != nil {
		return result, false, err
	}
	return result, true, consumeStreamInto(attemptCtx, events, hooks, result, idleTimeout, logger)
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
