// executor_stream.go — per-turn LLM stream consumption: turnResult
// accumulation, the idle-timeout watchdog (ErrStreamIdle), and streaming
// hook dispatch. Split from executor.go (RunAgent core loop).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type turnResult struct {
	text           string
	stopReason     string
	toolCalls      []llm.ContentBlock
	contentBlocks  []llm.ContentBlock
	usage          llm.TokenUsage
	providerModel  string
	maxStreamBytes int
	streamBytes    int
	contentStarted bool
}

// defaultStreamIdleTimeout is the default maximum wait for the next SSE event
// during LLM streaming. Set above Claude Code's 90s default because local vLLM
// models (step3p7) have a slow cold start (~80s) and slow prefill on large
// contexts; at 90s their first token can miss the window and trip a false idle
// stall, which retried and failed cron runs (email-realtime auto-disabled after
// 10 such errors). Fast hosted APIs (GLM via Z.ai) stream well within this, so
// the higher ceiling only delays detection of a genuine hang.
const defaultStreamIdleTimeout = 180 * time.Second

// ErrStreamIdle is returned when the LLM stream stalls (no event within the
// idle timeout). The error is considered retryable by callers.
var ErrStreamIdle = fmt.Errorf("stream stalled: no event within idle timeout")

// streamIdleTimeoutOverride reads DENEB_STREAM_IDLE_TIMEOUT_MS once. The
// watchdog deliberately measures real progress events — keepalive pings and
// SSE comments do NOT reset it (see forwardAnthropicStream) — so a consumer
// that legitimately holds a request without emitting events (the puppet
// broker, where a coding agent sits in the model seat and thinks for minutes)
// must widen or disable it explicitly: positive ms overrides the default,
// negative disables, unset/invalid keeps the default.
var streamIdleTimeoutOverride = sync.OnceValue(func() time.Duration {
	v := strings.TrimSpace(os.Getenv("DENEB_STREAM_IDLE_TIMEOUT_MS"))
	if v == "" {
		return 0
	}
	ms, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	if ms < 0 {
		return -1 // disable sentinel (any negative duration disables)
	}
	return time.Duration(ms) * time.Millisecond
})

// effectiveIdleTimeout resolves the stream idle timeout: an explicit config
// value wins, then the env override, then the default. Negative = disabled.
func effectiveIdleTimeout(cfg time.Duration) time.Duration {
	if cfg != 0 {
		return cfg
	}
	if o := streamIdleTimeoutOverride(); o != 0 {
		return o
	}
	return defaultStreamIdleTimeout
}

// ErrStreamEvent is returned when the provider emits an explicit error event
// mid-stream (upstream disconnect, transient backend fault, overload). Like
// ErrStreamIdle it is considered retryable: mid-stream errors are almost
// always transient (permanent faults reject the request at the HTTP layer,
// before streaming starts), so callers retry the turn once on the same model
// before escalating to the model-fallback chain.
var ErrStreamEvent = errors.New("stream reported error event")

// ErrStreamLimit is returned before a translated provider stream can exceed
// the run-scoped byte budget configured by a deterministic evaluator.
var ErrStreamLimit = errors.New("stream exceeded configured byte limit")

// consumeStreamInto reads all events from a streaming LLM response and
// populates the provided turnResult.
//
// idleTimeout controls how long to wait for the next event before declaring
// the stream stalled. Zero uses defaultStreamIdleTimeout; negative disables.
func consumeStreamInto(ctx context.Context, events <-chan llm.StreamEvent, hooks StreamHooks, result *turnResult, idleTimeout time.Duration, logger *slog.Logger) error {
	idleTimeout = effectiveIdleTimeout(idleTimeout)
	accumulator := newStreamAccumulator(result, hooks, logger)

	// Idle watchdog: detects LLM stream stalls where the TCP connection stays
	// alive but no SSE events arrive. Without this, stalled streams hang
	// indefinitely (HTTP-level timeouts are too coarse at 5+ minutes).
	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	if idleTimeout > 0 {
		idleTimer = time.NewTimer(idleTimeout)
		defer idleTimer.Stop()
		idleCh = idleTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idleCh:
			return ErrStreamIdle
		case ev, ok := <-events:
			if !ok {
				// Channel closed without message_stop — truncated stream.
				accumulator.flushTruncated()
				return nil
			}
			// Reset idle watchdog on every received event.
			if idleTimer != nil {
				if !idleTimer.Stop() {
					// Drain channel if timer already fired (race window).
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(idleTimeout)
			}

			complete, err := accumulator.apply(ev)
			if err != nil {
				return err
			}
			if complete {
				return nil
			}
		}
	}
}
