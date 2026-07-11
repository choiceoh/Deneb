// executor.go — Core agent execution loop.
//
// RunAgent implements the LLM → tool-call → repeat cycle shared by both the
// chat pipeline (chat/) and the auto-reply pipeline (autoreply/). Turn request
// preparation and streaming mechanics live in adjacent executor_* files;
// callers only need thin adapters that map their domain-specific config to
// AgentConfig.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

// toolHeartbeatInterval is how often OnToolProgress fires while a single
// tool call is still executing. Chosen to comfortably fit under the 30s
// typing-indicator TTL (see autoreply/typing.TypingController) so long
// (compile, test-suite, network fetch) tool calls do not let the surface
// liveness indicator lapse. Exported as a var for tests that want to
// shrink the interval.
var toolHeartbeatInterval = 10 * time.Second

// deliverableNarrationMaxRunes bounds how long a tool-bearing turn's text may be
// while still counting as interim progress narration ("이제 위키 검색부터 할게요")
// rather than answer content. A turn that calls tools and stays under this is
// excluded from AgentResult.DeliverableText; terminal turns (no tool calls) and
// long content turns are always kept. Calibrated from observed cron transcripts:
// narration peaked near 83 runes while the shortest tool-accompanied answer ran
// ~2300 runes, so 300 sits in a wide safe gap.
const deliverableNarrationMaxRunes = 300

// RunAgent executes the agent tool-call loop: call LLM → detect tool_use →
// execute tool → feed result → repeat until the model stops or limits are hit.
//
// client must satisfy LLMStreamer (*llm.Client does).
// tools may be nil if no tool use is expected.
// hooks provides optional callbacks for streaming events; pass zero-value if not needed.
// runLog may be nil; if provided it records per-turn LLM and tool events.
func RunAgent(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client LLMStreamer,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*AgentResult, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 25
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	// Every turn is decorated from the same run-scoped base. Reusing the prior
	// turn's decorated context retains per-turn values (including potentially
	// large tool results) until the whole run ends and grows Value lookup depth.
	runCtx := ctx

	state := newAgentRunState(messages, cfg.OnMessagePersist)
	result := state.result
	journal := state.journal
	defer state.finalize()

	baseMaxTokens := cfg.MaxTokens // Original value before any recovery scaling.
	turnPreparer := newTurnRequestPreparer(&cfg)

	// Loop condition permits one extra iteration past MaxTurns when the grace
	// flag is set — see grace.go. Normal runs behave identically to `range
	// cfg.MaxTurns`; the grace branch only engages on budget exhaustion.
	// BudgetGraceCall is armed at the bottom of the final budgeted turn (see
	// the injection block at loop tail) and cleared there or on the early
	// end_turn return path.
	for turn := 0; turn < cfg.MaxTurns || result.BudgetGraceCall; turn++ {
		result.Turns = turn + 1

		prepared := turnPreparer.prepare(runCtx, turn, journal.messages, result.ToolActivities)
		ctx = prepared.ctx
		turnThinking := prepared.thinking

		streamOutcome, err := runStreamingTurnWithRetry(
			ctx, client, prepared.request, hooks, cfg.StreamIdleTimeout, logger, turn,
		)
		result.Stream.record(streamOutcome)
		if err != nil {
			if ctx.Err() != nil {
				result.Stream.TerminationReason = string(streamTerminationContextDone)
				result.StopReason = stopReasonFromCtx(ctx)
				return result, nil
			}
			if streamOutcome.initialConnectionFailed() {
				return nil, fmt.Errorf("stream chat (turn %d): %w", turn, err)
			}
			return nil, fmt.Errorf("consume stream (turn %d): %w", turn, err)
		}
		turnRes := streamOutcome.result

		// Commit the completed provider turn to run-level usage/text/tool
		// aggregates before any hook or recovery decision observes the result.
		turnStats := state.recordTurn(turnRes)

		// Per-turn token logging: surface per-turn cost so multi-turn runs
		// are transparent (the accumulated total can be misleading).
		// Also record text/tool split so post-hoc diagnosis can tell whether a
		// turn produced deliverable prose or only tool calls — crucial when
		// investigating "agent composed body as tool-call argument vs. as text"
		// questions (the 4/24 19:35 email-analysis-full wrap-up postmortem had
		// to guess this from token counts alone).
		// textHead gives a 200-byte, rune-safe window into the turn's prose
		// output. It distinguishes "the agent composed the deliverable here"
		// from "the agent only emitted a status line".
		// The 19:35 wrap-up bug would have been a 5-second grep with this field:
		// look for a turn where textChars is large AND textHead is not 'wiki
		// 업데이트 완료'.
		logger.Info("agent turn complete",
			"turn", turn,
			"turnInputTokens", turnRes.usage.InputTokens,
			"turnOutputTokens", turnRes.usage.OutputTokens,
			"turnCacheReadTokens", turnRes.usage.CacheReadInputTokens,
			"turnCacheCreationTokens", turnRes.usage.CacheCreationInputTokens,
			"accInputTokens", result.Usage.InputTokens,
			"messages", len(journal.messages),
			"textChars", turnStats.textChars,
			"textHead", turnStats.textHead,
			"toolCount", turnStats.toolCount,
			"toolNames", strings.Join(turnStats.toolNames, ","),
			"toolInputBytes", turnStats.toolInputBytes,
			"stopReason", turnRes.stopReason)

		// Feed actual token usage back to the estimator for self-calibration.
		if turnRes.usage.InputTokens > 0 {
			est := tokenest.ForModel(cfg.Model)
			estimated := est.CountBytes([]byte(prepared.request.System))
			for _, m := range prepared.request.Messages {
				estimated += est.CountBytes([]byte(m.Content))
			}
			tokenest.RecordFeedback(est.Family(), estimated, turnRes.usage.InputTokens)
		}

		// Log LLM turn result to agent detail log. ThinkingOff/ObsRunes record
		// the per-step effort decision (turnThinking) and the observation size
		// that informed it (run-scoped tool-result runes seen so far) — the
		// label feed for a future learned router.
		if runLog != nil {
			runLog.LogTurnLLM(agentlog.TurnLLMData{
				Turn:                turn + 1,
				InputTokens:         turnRes.usage.InputTokens,
				OutputTokens:        turnRes.usage.OutputTokens,
				StopReason:          turnRes.stopReason,
				TextLen:             len(turnRes.text),
				ToolCalls:           len(turnRes.toolCalls),
				CacheReadTokens:     turnRes.usage.CacheReadInputTokens,
				CacheCreationTokens: turnRes.usage.CacheCreationInputTokens,
				ThinkingOff:         turnThinking != nil && turnThinking.Type == "disabled",
				ObsRunes:            state.priorToolOutputRunes,
			})
		}

		// Mid-run hook: notify caller of token accumulation.
		if cfg.OnTurn != nil {
			cfg.OnTurn(turn+1, result.Usage.InputTokens+result.Usage.OutputTokens)
		}

		// Build the assistant message once. Recovery and tool turns append it to
		// the next request's history; finishing turns persist it without append.
		// The staged wrapper keeps the finalize gate's pre-decision persist and
		// later append on a single, exactly-once persistence path.
		assistantMessage := journal.stage(llm.NewBlockMessage("assistant", turnRes.contentBlocks))

		// --- Max-output-tokens recovery ---
		// When the LLM response is truncated by max_tokens (not a clean end_turn),
		// inject a "resume" message and retry. This prevents losing partially
		// generated code or explanations.
		if turnRes.stopReason == "max_tokens" && len(turnRes.toolCalls) == 0 &&
			cfg.MaxOutputTokensRecovery > 0 && state.maxTokenRecoveries < cfg.MaxOutputTokensRecovery {
			recoveryAttempt := state.noteRecovery()

			// Thinking runaway: the turn produced ONLY reasoning and no answer text,
			// burning the whole output budget on the thinking channel. Scaling the
			// budget just lets it reason even longer (dsv4 can't lower effort), so
			// retry the turn with thinking OFF (resetting the budget) — it then
			// answers directly. Only when the caller supplied an off-config.
			if cfg.ThinkingOffRetry != nil && strings.TrimSpace(turnRes.text) == "" &&
				turnStats.thinkingText != "" {
				turnPreparer.overrideThinkingOnce(cfg.ThinkingOffRetry)
				cfg.MaxTokens = baseMaxTokens
				logger.Info("max_tokens recovery: thinking runaway — retrying with thinking off",
					"attempt", recoveryAttempt, "maxAttempts", cfg.MaxOutputTokensRecovery)
				assistantMessage.append()
				journal.append(llm.NewTextMessage("user",
					"[직전 응답이 분석(생각)만 하다 토큰 한도에 걸렸습니다. 추가 분석 없이 곧바로 최종 답변만 작성하세요.]"))
				continue
			}

			// Genuine truncated answer: scale up MaxTokens so the model has room to
			// finish, and resume from where it left off.
			scale := 2.0 // Default: double the original.
			if idx := recoveryAttempt - 1; idx < len(cfg.MaxOutputTokensScaleFactors) {
				scale = cfg.MaxOutputTokensScaleFactors[idx]
			}
			cfg.MaxTokens = int(float64(baseMaxTokens) * scale)

			logger.Info("max_tokens recovery: scaling output tokens and injecting resume",
				"attempt", recoveryAttempt,
				"maxAttempts", cfg.MaxOutputTokensRecovery,
				"baseMaxTokens", baseMaxTokens,
				"newMaxTokens", cfg.MaxTokens)
			// Append the truncated assistant output so the LLM sees what it already wrote.
			assistantMessage.append()
			// Inject a user-role resume prompt.
			journal.append(llm.NewTextMessage("user",
				"[Output was truncated due to token limit. Resume directly from where you left off — no apology, no recap.]"))
			continue
		}

		// A finish attempt (clean end_turn or a turn with no tool calls) may be
		// held by the verification gate below. Persist the finishing assistant
		// turn HERE, before consulting the gate, for two reasons: (1) the chat
		// wiring's persister also feeds the gate this turn's text (so an explicit
		// "verification not applicable" opt-out is recognized on the SAME turn
		// the model tries to end, never nagged after a valid reason); (2) whether
		// the gate then holds or lets the finish through, the turn is recorded
		// exactly once — the staged message is idempotent when a held finish later
		// moves into history. Non-finish turns append via the normal path below.
		finishAttempt := turnRes.stopReason == "end_turn" || len(turnRes.toolCalls) == 0
		if finishAttempt && turnRes.text != "" {
			assistantMessage.persist()
		}

		// --- Verification gate: hold a finish that skipped verification ---
		// Mirrors the max_tokens recovery shape: append what the model said,
		// inject a user-role demand, keep looping. Unlike the max_tokens
		// recovery, the gate may escalate and HARD-BLOCK — it can keep returning
		// a demand across finish attempts until the run verifies or explicitly
		// opts out, so only the loop's own budget (max_turns) bounds it. The
		// gate inspects this finishing turn's text (e.g. for an explicit opt-out
		// line) via the OnMessagePersist feed above, so it decides on the SAME
		// turn the model tries to end.
		//
		// EXCEPTION — the grace turn: when we are already in the single grace
		// iteration past max_turns (BudgetGraceCall), there is no budget left to
		// run a verification, and holding here would keep the grace flag alive
		// and loop forever. So on the grace turn we do NOT hold; we probe the
		// gate with the terminal sentinel (-1) so a still-armed escape is still
		// logged, then let the finish through.
		if finishAttempt && cfg.FinalizeGate != nil {
			if result.BudgetGraceCall {
				_ = cfg.FinalizeGate(-1) // log-only; never hold past the grace turn
			} else if gatePrompt := cfg.FinalizeGate(turn); gatePrompt != "" {
				logger.Info("finalize gate: holding finish for verification", "turn", turn)
				assistantMessage.append()
				journal.append(llm.NewTextMessage("user", gatePrompt))
				continue
			}
		}

		// --- Check stop reason ---
		if finishAttempt {
			// Persist the terminal assistant message (not appended to messages
			// since the loop is ending, but must be in transcript for next run).
			// Idempotent when the finish-attempt path above already recorded it.
			if turnRes.text != "" {
				assistantMessage.persist()
			}

			// Grace iteration produced its wrap-up reply — surface the graceful
			// stop reason so callers can distinguish "budget forced the close"
			// from a spontaneous end_turn.
			if result.BudgetGraceCall {
				result.StopReason = StopReasonMaxTurnsGraceful
				result.BudgetGraceCall = false
			} else {
				result.StopReason = turnRes.stopReason
				if result.StopReason == "" {
					result.StopReason = "end_turn"
				}
			}
			return result, nil
		}

		// After turn 0 completes and more turns follow, strip base64 image data from
		// the message history to avoid retransmitting image bytes on every subsequent
		// turn. The image was already consumed by the model on turn 0; subsequent turns
		// only need the text context. Each image block (~1600 tokens) becomes a tiny
		// text placeholder instead.
		if turn == 0 && cfg.StripImagesAfterFirstTurn {
			journal.messages = stripBase64ImagesFromHistory(journal.messages)
		}

		// Record where the current turn's messages begin in the array.
		// Everything before this index is from prior turns and eligible for
		// tool result compaction.
		currentTurnStart := len(journal.messages)

		// Tool turns continue the conversation, so append the staged assistant
		// message before dispatching its calls.
		assistantMessage.append()

		// Execute the complete tool turn into a commit-ready outcome. The helper
		// preserves real results and fills calls skipped by cancellation with
		// synthetic error results, so every persisted tool_use remains paired.
		toolTurn := executeToolTurn(
			ctx,
			cfg,
			turnRes.toolCalls,
			tools,
			hooks,
			extractThinkingText(turnRes.contentBlocks),
			turn,
			logger,
			runLog,
		)
		state.recordToolActivities(toolTurn.activities)

		// Post-turn hook: skill nudger (and future accounting that needs
		// the turn's tool activities). Fires even when the turn had no
		// tool calls so subscribers can track turn progression.
		if cfg.OnToolTurn != nil {
			cfg.OnToolTurn(turn+1, toolTurn.activities)
		}

		toolResults := toolTurn.results
		if !toolTurn.canceled {
			// Per-path edit-thrash nudges and budget warnings only matter when a
			// later LLM turn will consume them. Omit both from canceled history.
			for _, nudge := range toolTurn.editThrashNudges {
				toolResults = append(toolResults, llm.ContentBlock{
					Type: "text",
					Text: nudge,
				})
			}
			toolResults = turnPreparer.appendBudgetWarning(turn, toolResults)
		}

		toolResultMsg := llm.NewBlockMessage("user", toolResults)
		journal.append(toolResultMsg)

		// Cancellation returns only after the same activity/result commit used
		// by normal turns. This keeps FinalMessages and the durable transcript
		// balanced while retaining successful calls that finished before abort.
		if toolTurn.canceled {
			result.StopReason = stopReasonFromCtx(ctx)
			result.InterruptedToolNames = append(result.InterruptedToolNames, toolTurn.interruptedNames...)
			return result, nil
		}

		// Prior-turn tool result compaction: shrink tool_result content from
		// completed turns to CompactedMaxOutput (4K chars). The LLM already
		// saw the full result on the turn it was produced; subsequent turns
		// only need a summary. This prevents multi-turn token explosion where
		// resending full tool results (32K each) on every turn compounds cost.
		if n := CompactPriorToolResults(journal.messages, currentTurnStart); n > 0 {
			logger.Info("compacted prior tool results",
				"turn", turn,
				"blocksCompacted", n)
		}

		switch {
		case result.BudgetGraceCall:
			// Grace iteration just completed (we extended past MaxTurns for a
			// single wrap-up turn). Clear the flag so the loop guard fails
			// next check — the terminal return below sets the graceful marker.
			result.BudgetGraceCall = false

		case turn+1 >= cfg.MaxTurns && !result.BudgetExhaustedInjected:
			// This was the final budgeted turn AND the model chose to keep
			// calling tools (if it had emitted end_turn / no tools, the
			// early-exit branch above would already have returned). Inject
			// ONE wrap-up user message now so the next iteration has it in
			// history, and arm BudgetGraceCall to extend the loop by exactly
			// one iteration. The injection is an append-only operation so
			// prompt cache up through the just-recorded tool_result is
			// preserved (no mutation of prior messages).
			journal.append(llm.NewTextMessage("user", GraceCallPrompt))
			result.BudgetExhaustedInjected = true
			result.BudgetGraceCall = true
			logger.Warn("agent turn budget exhausted; issuing grace wrap-up call",
				"maxTurns", cfg.MaxTurns, "turn", turn)
		}
	}

	// Terminal gate probe: the loop fell through to max_turns (not a clean,
	// gate-cleared end_turn), so the finish gate was never given the last word.
	// A still-armed gate at this point is a silent escape — a run that mutated
	// files, never verified, and ran out of budget. Consult the gate one final
	// time with a sentinel turn (-1 = "terminal probe, do not inject") purely
	// for its side-effect logging; the returned prompt is discarded because the
	// loop is over. Gate-agnostic by construction: any FinalizeGate that ignores
	// the sentinel simply returns a prompt we drop.
	if cfg.FinalizeGate != nil {
		_ = cfg.FinalizeGate(-1)
	}

	if result.BudgetExhaustedInjected {
		result.StopReason = StopReasonMaxTurnsGraceful
	} else {
		result.StopReason = "max_turns"
	}
	return result, nil
}

// turnResult holds the parsed output of a single LLM turn.
