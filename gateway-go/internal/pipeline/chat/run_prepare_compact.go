// run_prepare_compact.go — message assembly, Polaris compaction, prompt
// finalization, and the local-AI summarizer. Extracted from run_prepare.go
// under the 700-LOC rule. Called sequentially after the context/prompt
// preparation stages in run_prepare.go.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// assembleMessages builds the final message list from prebuilt messages, transcript
// context, attachments, and Polaris compaction. mr identifies the resolved
// provider/model so compaction budgets and content handling can respect the
// model's capabilities (context window, vision).
func assembleMessages(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	prep prepResult,
	mr modelResolution,
	logger *slog.Logger,
	hooks *compactionHooks,
) []llm.Message {
	messages := assembleTurnMessages(ctx, params, deps, prep, mr)

	// Polaris compaction: tiered context compression — see compactTurnMessages.
	if len(messages) > 0 {
		messages = compactTurnMessages(ctx, params, deps, mr, messages, logger, hooks)
	}
	return messages
}

// assembleTurnMessages builds the turn's raw message list: transcript
// history (or caller-prebuilt messages), the current user message, extracted
// document attachments, and the non-vision image strip.
func assembleTurnMessages(ctx context.Context, params RunParams, deps runDeps, prep prepResult, mr modelResolution) []llm.Message {
	messages := prep.Messages

	// Extract raw document attachments (PDF/Office/CSV the native client sends as
	// base64 bytes with no explicit type) into text up front, so the block
	// builders below render their content instead of silently dropping them.
	attachments := prepareDocumentAttachments(ctx, params.Attachments)

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		// Copy to avoid aliasing the caller's backing array. Without the copy,
		// append may write into shared capacity, corrupting the original slice.
		messages = append([]llm.Message(nil), params.PrebuiltMessages...)
		// When the caller also supplies a Message, append it so the LLM sees
		// it without re-loading the entire transcript.
		if params.Message != "" && len(attachments) == 0 {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	}

	// Build or augment user message with attachments.
	switch {
	case len(messages) == 0 && params.Message != "":
		// No history — build the user message from scratch.
		if len(attachments) > 0 {
			blocks := buildAttachmentBlocks(params.Message, attachments)
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	case params.AppendCurrentMessage && params.Message != "":
		// The current turn's message is not in the loaded history — its
		// persist was deferred to the enrichment join, or it is an ephemeral
		// trigger that never persists (run_exec.go). Append it as a NEW last
		// user message; the replace-last branch below would corrupt the
		// PREVIOUS turn's message here.
		if len(attachments) > 0 {
			messages = append(messages, llm.NewBlockMessage("user", buildAttachmentBlocks(params.Message, attachments)))
		} else {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	case len(messages) > 0 && len(attachments) > 0:
		// History exists but current message has attachments — replace the
		// last user message (which was persisted as text-only) with a
		// multimodal version that includes the image/video content blocks.
		messages = appendAttachmentsToHistory(messages, params.Message, attachments)
	}

	// Model marked non-vision (provider config `vision: false`): replace image
	// blocks with text stubs up front instead of letting the provider reject
	// the request. Only fires on an explicit override — unknown models are
	// assumed vision-capable.
	if modelCapability(deps, mr.providerID, mr.model).NoVision {
		messages = compact.StripImageBlocks(messages)
	}
	return messages
}

// Polaris compaction: tiered context compression.
// Applied after message assembly, before prompt finalization.
// STW (Stop-the-World): when LLM compaction fires, the user sees a
// ✍ status emoji and typing keepalive until compaction completes.
// No LLM call is made until context is compressed — incoming messages
// are already queued by PendingQueue during the active run.
func compactTurnMessages(ctx context.Context, params RunParams, deps runDeps, mr modelResolution, messages []llm.Message, logger *slog.Logger, hooks *compactionHooks) []llm.Message {
	// Derive compaction budget from context assembly budgets so they stay
	// in sync, clamped to the model's context window when it is known.
	contextBudget := effectiveContextBudget(deps, mr.providerID, mr.model, logger)

	// History-suppressed runs (skill-review forks pass MaxHistoryTokens=1
	// to exclude transcript history) yield a sub-floor budget no compaction
	// can meet: the protected current turn alone exceeds it, so every tier
	// runs for nothing and the "failed to reduce below budget" warning
	// fires on each run. Budget 0 means "no budget configured" and keeps
	// the legacy run-everything behavior.
	if skipCompactionBudget(contextBudget) {
		logger.Debug("polaris: budget below compaction floor; skipping compaction",
			"session", params.SessionKey, "budget", contextBudget)
		return messages
	}

	// syncCompactionStall bounds the in-turn (STW) compaction — the backstop
	// for cases that cannot defer to the background pass (first compaction,
	// models with no window headroom, the hard ceiling). Raised 2m→3m so the
	// parallel chunk summaries have room to finish when the analysis model is
	// slow under GPU contention (the "polaris: chunk summarization failed …
	// context deadline exceeded" warnings), rather than failing and re-running
	// the same first compaction every turn. Stays well under the 5m turn
	// deadline. Trade-off: a turn that triggers synchronous compaction can
	// stall up to this long before replying; the deferred background path
	// (5m, off the critical path) already absorbs the common case.
	const syncCompactionStall = 3 * time.Minute
	polarisCtx, polarisCancel := context.WithTimeout(ctx, syncCompactionStall)
	var summarizer compact.Summarizer
	if pilotHub := pilot.LocalAIHub(); pilotHub != nil && !deps.briefcaseMode {
		summarizer = &localAISummarizer{}
	}

	// Off-critical-path compaction (defer the STW): when the assembled raw
	// history is over the LLM threshold but still fits the model's context
	// window with headroom — large-window local models (e.g. dsv4), whose
	// decode rate is flat well past the configured budget — run THIS turn on
	// the raw context and summarize in the BACKGROUND instead of blocking the
	// agent loop on a multi-second STW summarization. The next turn assembles
	// the background-persisted summary. The synchronous path below stays the
	// backstop for: the first compaction (no summaries yet → AssembleContext
	// truncated the tail, which CompactAndPersist must recover), models whose
	// window has no headroom over the budget, and the hard ceiling where the
	// raw history would not fit the window. Re-prefill behaviour is unchanged
	// (the summary still lands one turn later); only the STW is removed. See
	// polaris.Engine.CompactInBackground and prompt-cache.md §1.5.
	if bridge, ok := deps.transcript.(*polaris.Bridge); ok && summarizer != nil {
		engine := bridge.Engine()
		currentTokens := compact.EstimateMessagesTokens(messages)
		softThreshold := int(float64(contextBudget) * compact.DefaultLLMThresholdPct)
		ceiling := contextWindowCeiling(deps, mr.providerID, mr.model)
		deferEligible := currentTokens > softThreshold &&
			ceiling > contextBudget && // model window clearly exceeds the budget
			currentTokens <= ceiling && // raw history fits the window with reserve
			engine.HasSummaries(params.SessionKey) // past bootstrap; tail is assembled raw
		if deferEligible {
			engine.CompactInBackground(
				deps.callbacks.shutdownCtx, params.SessionKey, summarizer, contextBudget,
				deps.memory.Embedding, buildAnchorKeywords(deps.memory.Wiki), buildLearnedGuidelines(),
			)
			// Belt-and-suspenders: never ship an orphan tool pair at the
			// assembly's coverage boundary (e.g. a prior chunk-boundary
			// leftover). No-op — byte-identical, APC-stable — when already
			// balanced; operates on this turn's working list, not the store.
			messages = compact.BalanceToolBlocks(messages)
			polarisCancel()
			logger.Info("polaris: deferred compaction to background (turn runs on raw context)",
				"session", params.SessionKey, "tokens", currentTokens,
				"budget", contextBudget, "ceiling", ceiling)
			return messages
		}
	}

	// STW: pre-check if LLM compaction will likely fire.
	// Signal the user before the (potentially slow) summarization starts.
	var compactTypingDone chan struct{}
	var compactStart time.Time
	if hooks != nil && summarizer != nil {
		currentTokens := compact.EstimateMessagesTokens(messages)
		threshold := int(float64(contextBudget) * compact.DefaultLLMThresholdPct)
		if currentTokens > threshold {
			compactStart = time.Now()
			logger.Info("pipeline: STW compaction starting",
				"tokens", currentTokens, "budget", contextBudget,
				"ratio", fmt.Sprintf("%.1f%%", float64(currentTokens)/float64(contextBudget)*100))
			if hooks.typingFn != nil {
				compactTypingDone = make(chan struct{})
				typingFn := hooks.typingFn
				typingLogger := logger
				go func() {
					defer func() {
						if r := recover(); r != nil {
							typingLogger.Error("panic in compaction typing loop", "panic", r)
						}
					}()
					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-compactTypingDone:
							return
						case <-ctx.Done():
							return
						case <-ticker.C:
							typingFn()
						}
					}
				}()
			}
		}
	}

	var polarisResult compact.Result
	if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
		engine := bridge.Engine()
		if deps.memory.Embedding != nil {
			engine.SetEmbedder(deps.memory.Embedding)
		}
		if deps.briefcaseMode {
			engine.SetAnchorKeywords(nil)
			engine.SetLearnedGuidelines(nil)
		} else {
			engine.SetAnchorKeywords(buildAnchorKeywords(deps.memory.Wiki))
			engine.SetLearnedGuidelines(buildLearnedGuidelines())
		}
		messages, polarisResult = engine.CompactAndPersist(polarisCtx, params.SessionKey, messages, summarizer, contextBudget)

		// Proactive condensation: when a new leaf summary was persisted,
		// trigger background condensation to merge leaves into higher-level nodes.
		// Runs in its own goroutine with a bounded timeout so it cannot
		// outlive sensible lifetime and cannot take down the process on panic.
		if polarisResult.LLMCompacted && summarizer != nil {
			condSummarizer := summarizer // capture for goroutine
			sessionKey := params.SessionKey
			condLogger := logger
			// Decouple from the request ctx so Condense outlives the agent turn,
			// but derive from the server shutdown ctx so a graceful shutdown
			// cancels it. Falls back to Background if shutdownCtx isn't wired
			// yet (e.g. in tests) — still bounded by the timeout below.
			parentCtx := deps.callbacks.shutdownCtx
			if parentCtx == nil {
				parentCtx = context.Background()
			}
			go func() { //nolint:gosec // G118 — decoupled from request ctx on purpose; bounded timeout below
				defer func() {
					if r := recover(); r != nil {
						condLogger.Error("panic in background condense", "session", sessionKey, "panic", r)
					}
				}()
				// Bounded by a 5-minute timeout so it cannot leak forever.
				condCtx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
				defer cancel()
				if err := engine.Condense(condCtx, sessionKey, condSummarizer); err != nil {
					condLogger.Warn("background condense failed", "session", sessionKey, "error", err)
				}
			}()
		}
	} else {
		cfg := compact.NewConfig(contextBudget)
		cfg.Embedder = deps.memory.Embedding
		// Incremental recompaction: feed the prior summary so the LLM tier
		// UPDATES it (In Progress → Done) instead of re-summarizing from
		// scratch, then store the new summary for next time. In-memory on
		// the session; a /reset (new Session) or restart clears it.
		var compSession *session.Session
		if deps.sessions != nil {
			compSession = deps.sessions.Get(params.SessionKey)
		}
		if compSession != nil {
			cfg.PreviousSummary = compSession.PreviousCompactionSummary
		}
		messages, polarisResult = compact.Compact(polarisCtx, cfg, messages, summarizer, logger)
		if compSession != nil && polarisResult.Summary != "" {
			compSession.PreviousCompactionSummary = polarisResult.Summary
		}
	}
	polarisCancel()

	if compactTypingDone != nil {
		close(compactTypingDone)
	}
	if !compactStart.IsZero() {
		logger.Info("pipeline: STW compaction done",
			"durationMs", time.Since(compactStart).Milliseconds())
	}

	if polarisResult.MicroPruned > 0 || polarisResult.LLMCompacted || polarisResult.EmbeddingCompacted || polarisResult.RecencyCompacted || polarisResult.EmergencyEvicted > 0 {
		var tier string
		switch {
		case polarisResult.EmergencyEvicted > 0:
			tier = "emergency"
		case polarisResult.LLMCompacted:
			tier = "tier1-llm"
		case polarisResult.EmbeddingCompacted:
			tier = "tier2-embedding-mmr"
		case polarisResult.RecencyCompacted:
			tier = "tier3-recency"
		default:
			tier = "micro"
		}
		attrs := []any{"tokensBefore", polarisResult.TokensBefore, "tokensAfter", polarisResult.TokensAfter}
		if polarisResult.MicroPruned > 0 {
			attrs = append(attrs, "pruned", polarisResult.MicroPruned)
		}
		if polarisResult.EmergencyEvicted > 0 {
			attrs = append(attrs, "evicted", polarisResult.EmergencyEvicted)
		}
		logger.Info("polaris "+tier+" compaction", attrs...)
	}

	// P4: mark the session so the next turn's system prompt includes
	// a one-time reminder that summaries are present in history.
	// Cheap-pruning-only results (Micro, TruncateOldToolResults) do
	// not trigger this — see compactionProducedSummary in
	// chat/compaction_marker.go.
	markCompactionFired(deps, params.SessionKey, polarisResult)

	// Compaction ran (triggered by tokens > budget) but did not bring
	// tokens back within budget — degraded context state. Agent will
	// likely hit provider-side overflow; surface to operator now so we
	// know why a turn later fails, rather than blaming only the LLM.
	// Skip when budget is unset/zero (e.g. boot session, subagent) —
	// the inequality is trivially true and the warning becomes noise.
	if contextBudget > 0 && polarisResult.TokensBefore > contextBudget && polarisResult.TokensAfter > contextBudget {
		logger.Warn("polaris: compaction failed to reduce below budget",
			"session", params.SessionKey,
			"tokensBefore", polarisResult.TokensBefore,
			"tokensAfter", polarisResult.TokensAfter,
			"budget", contextBudget)
		if deps.broadcast != nil {
			deps.broadcast("chat.compaction_degraded", ChatCompactionDegradedEvent{
				Session:      params.SessionKey,
				TokensBefore: polarisResult.TokensBefore,
				TokensAfter:  polarisResult.TokensAfter,
				Budget:       contextBudget,
			})
		}
	}
	return messages
}

// finalizePrompt applies budget optimization and tier-1 wiki injection to the
// system prompt. recallAddition is normally "" — per-turn recall rides the
// last user message now (run_tail_inject.go) so the system prompt stays a
// stable vLLM APC prefix; it is only non-empty on the degenerate
// no-user-message fallback path.
func finalizePrompt(
	systemPrompt json.RawMessage,
	recallAddition string,
	tier1Addition string,
	contextCfg ContextConfig,
	sessionToolPreset string,
	message string,
) json.RawMessage {
	// Budget-optimize variable prompt additions before appending.
	if recallAddition != "" {
		// Current-turn recall evidence is compact and more relevant than
		// always-on tier-1 memory, so keep it even when the static prompt is
		// already at its nominal budget.
		systemPrompt = llm.AppendSystemText(systemPrompt, recallAddition)
	}

	if tier1Addition != "" {
		promptBudget := prompt.PromptBudget{Total: contextCfg.SystemPromptBudget}
		baseTokens := uint64(tokenest.Estimate(string(systemPrompt)))
		var remainingBudget uint64
		if promptBudget.Total > baseTokens {
			remainingBudget = promptBudget.Total - baseTokens
		}
		if promptBudget.Total > 0 && remainingBudget == 0 {
			return systemPrompt
		}
		additionBudget := prompt.PromptBudget{Total: remainingBudget}

		additionFragments := []prompt.PromptFragment{prompt.NewFragment("memory", tier1Addition)}
		optimized := additionBudget.Optimize(additionFragments)
		for _, f := range optimized {
			systemPrompt = llm.AppendSystemText(systemPrompt, f.Content)
		}
	}

	return systemPrompt
}

// agentConfigDeps holds dependencies specifically needed by buildAgentConfig.

var _ compact.Summarizer = (*localAISummarizer)(nil)

// localAISummarizer adapts the LOCAL lightweight model to the
// compaction.Summarizer interface. Compaction summarization had been routed to
// the analysis role, which now resolves to a cloud model (glm-5.2) — making each
// chunk summary a ~20s network round-trip that both burns subscription credits
// and is the dominant cause of the "polaris: chunk summarization failed …
// context deadline exceeded" timeouts (the sync 2m budget vs several chunks).
// The lightweight role (local qwen3.6-35b) summarizes the same chunk in ~3s with
// equal fact-fidelity and a more concise (better-compressed) result, so the
// background path is free and the sync path no longer times out.
type localAISummarizer struct{}

// Summarize produces a bounded summary of the supplied conversation.
func (s *localAISummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	return pilot.CallLocalLLM(ctx, system, conversation, maxOutputTokens)
}
