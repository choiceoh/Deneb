// zen_smt.go — Zen arch: Simultaneous Multithreading for session setup.
//
// CPU analogy: SMT allows multiple logical threads to share the same physical
// execution resources (ALU, cache, register file). One thread's stall (e.g.,
// waiting on memory) lets the other thread use the execution units.
//
// Application: runAgentAsync performs sequential setup (typing controller,
// status reactions, broadcaster, run logger) before calling executeAgentRun.
// SMT pattern: launch the agent's parallel prep phase (proactive context,
// knowledge prefetch) as "thread 1" while "thread 2" completes I/O-bound
// setup (typing indicator, emoji reaction API calls) on the same session.
//
// This module provides SMTSessionSetup which runs session setup steps
// concurrently, sharing the session's delivery context without contention
// (each step writes to its own output variable).
package chat

import (
	"context"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// SMTSetupResult holds the concurrently-initialized session setup components.
type SMTSetupResult struct {
	TypingSignaler *typing.FullTypingSignaler
	StatusCtrl     *channel.StatusReactionController
}

// SMTSessionSetup runs session setup steps concurrently using SMT-style
// resource sharing. Each "logical thread" operates on independent outputs
// while sharing the same delivery context and dependency set.
//
// Thread 1: typing controller setup (may involve I/O for initial typing signal)
// Thread 2: status reaction controller setup (may involve I/O for queued emoji)
//
// Both complete before the agent run starts. The overlap saves the latency
// of the slower I/O operation (typically 10-50ms for Telegram/Discord API calls).
func SMTSessionSetup(
	ctx context.Context,
	deps runDeps,
	params RunParams,
) SMTSetupResult {
	var result SMTSetupResult
	var wg sync.WaitGroup

	// Thread 1: typing indicator setup.
	if deps.typingFn != nil && params.Delivery != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delivery := params.Delivery
			typingCtrl := typing.NewTypingController(typing.TypingControllerConfig{
				OnStart:    func() { _ = deps.typingFn(ctx, delivery) },
				IntervalMs: 5000,
			})
			result.TypingSignaler = typing.NewFullTypingSignaler(typingCtrl, typing.TypingModeInstant, false)
			result.TypingSignaler.SignalRunStart()
		}()
	}

	// Thread 2: status reaction controller setup.
	if deps.reactionFn != nil && params.Delivery != nil && params.Delivery.MessageID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delivery := params.Delivery
			phaseEmojis := channel.StatusReactionEmojis{
				Queued:     "👀",
				Thinking:   "🤔",
				Tool:       "🔥",
				Coding:     "🔥",
				Web:        "⚡",
				Done:       "👍",
				Error:      "😱",
				StallSoft:  "🥱",
				StallHard:  "😨",
				Compacting: "🤔",
			}
			adapter := channel.StatusReactionAdapter{
				SetReaction: func(emoji string) error {
					rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					defer cancel()
					return deps.reactionFn(rctx, delivery, emoji)
				},
			}
			if deliveryChannel(params.Delivery) == "discord" && deps.removeReactionFn != nil {
				adapter.RemoveReaction = func(emoji string) error {
					rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					defer cancel()
					return deps.removeReactionFn(rctx, delivery, emoji)
				}
			}
			ctrl := channel.NewStatusReactionController(channel.StatusReactionControllerParams{
				Enabled: true,
				Adapter: adapter,
				Emojis:  &phaseEmojis,
				OnError: func(err error) {
					deps.logger.Warn("status reaction failed", "error", err)
				},
			})
			ctrl.SetQueued()
			result.StatusCtrl = ctrl
		}()
	}

	wg.Wait()
	return result
}
