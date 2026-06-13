// run_prepare.go — context/prompt preparation stages of the agent run:
// prepareContextAndPrompt (parallel recall+history+prompt), assembleMessages
// (compaction tiers + budget enforcement), finalizePrompt, and the local-AI
// summarizer they share. Called sequentially from executeAgentRun (run_exec.go).
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

type prepResult struct {
	Messages     []llm.Message
	SystemPrompt json.RawMessage
	RecallMemory string
	Tier1Wiki    string
	ContextErr   error
}

// prepareContextAndPrompt runs wiki injection, context assembly, and system prompt
// build in parallel. Returns the combined results.
//
// statusCtrl is optional: when non-nil, the recall goroutine signals
// SetRecalling on a true cache miss (cue present + no cached evidence) so
// the user sees 🧠 only when memory search is actually happening, not for
// every prep call.
func prepareContextAndPrompt(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	workspaceDir string,
	sessionToolPreset string,
	statusCtrl statusReactor,
	logger *slog.Logger,
) prepResult {
	var result prepResult
	var resultMu sync.Mutex
	var prepWg sync.WaitGroup

	// Tier-1 wiki auto-injection (parallel). Frozen per session (tier1_cache.go):
	// FormatTier1 reads the live store, and mid-session wiki writes would
	// otherwise shift the system-prompt tail every few turns — invalidating
	// the vLLM APC prefix for the tool schemas + entire history.
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-tier1-wiki", func() {
		defer prepWg.Done()
		var tier1 string
		if deps.wikiStore != nil {
			if cached, ok := cachedTier1Wiki(params.SessionKey); ok {
				tier1 = cached
			} else {
				cfg := wiki.ConfigFromEnv()
				tier1 = knowledge.FormatTier1(deps.wikiStore, cfg.Tier1MinImportance)
				storeTier1Wiki(params.SessionKey, tier1)
			}
		}
		resultMu.Lock()
		result.Tier1Wiki = tier1
		resultMu.Unlock()
	})

	// Recall preflight (parallel): inject focused memory before the LLM call.
	//
	// Two modes feed one <recall-context> block:
	//   - Cue-gated sources (wiki/diary/transcript/polaris) run only when the
	//     user message implies past context. Their result is cached per
	//     (session, cue-fingerprint) so repeat questions on the same topic
	//     reuse the ~6s of parallel search timeouts.
	//   - Hindsight auto-recall runs every turn when configured (the Hermes
	//     auto_recall model): the memory bank is queried with the current
	//     message regardless of cue. No-cue turns are not cached — each
	//     turn's message is a distinct query the "" fingerprint cannot
	//     disambiguate. /reset clears every slot. See chat/recall_cache.go.
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-recall", func() {
		defer prepWg.Done()
		// Ephemeral turns (autonomous heartbeat self-triggers) never run
		// recall — there is no real user message to recall against.
		if params.EphemeralUser {
			return
		}
		fingerprint := recallCueFingerprint(params.Message)
		hasCue := fingerprint != ""
		// Hermes-style auto_recall: run the preflight every turn, not just cue turns.
		// buildRecallPreflight searches wiki/diary/polaris/transcript/hindsight and returns
		// "" silently when there's no evidence, so non-cue turns add latency but no noise.
		if hasCue {
			if cached, ok := cachedRecallMemory(params.SessionKey, fingerprint); ok {
				resultMu.Lock()
				result.RecallMemory = cached
				resultMu.Unlock()
				return
			}
			// Explicit recall: surface the 🧠 phase so the user sees the
			// wiki/diary/transcript search instead of a frozen 📚. Silent
			// auto-recall on no-cue turns stays invisible.
			emitPhase(deps, params, "recalling", time.Now())
			if statusCtrl != nil {
				statusCtrl.SetRecalling()
			}
		}
		recallMemory, recallTruncated := buildRecallPreflight(ctx, params, deps, logger)
		if shouldFreezeRecallSnapshot(hasCue, recallTruncated, recallMemory) {
			storeRecallMemory(params.SessionKey, fingerprint, recallMemory)
		}
		resultMu.Lock()
		result.RecallMemory = recallMemory
		resultMu.Unlock()
	})

	// Context assembly (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-context", func() {
		defer prepWg.Done()

		var messages []llm.Message
		var contextErr error
		if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
			ctxResult, err := assembleContext(bridge, params.SessionKey, deps.contextCfg, logger)
			if err != nil {
				contextErr = err
			} else {
				messages = ctxResult.Messages
				// Log-only telemetry for truncation. Do NOT inject a synthetic
				// notice message here: bootstrapIfNeeded (inside CompactAndPersist)
				// recovers dropped messages by computing olderEnd from len(messages),
				// so any synthetic prepend inflates the count and orphans the
				// fresh-tail boundary message, causing "right-after-compaction
				// previous turn forgotten" regressions.
				if !ctxResult.WasCompacted && ctxResult.TotalMessages > len(ctxResult.Messages) && len(ctxResult.Messages) > 0 {
					logger.Warn("context truncated without summaries (bootstrap will recover)",
						"total", ctxResult.TotalMessages,
						"loaded", len(ctxResult.Messages),
						"dropped", ctxResult.TotalMessages-len(ctxResult.Messages),
						"session", params.SessionKey)
				}
			}
		}
		resultMu.Lock()
		result.Messages = messages
		result.ContextErr = contextErr
		resultMu.Unlock()
	})

	// System prompt build (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-sysprompt", func() {
		defer prepWg.Done()
		var systemPrompt json.RawMessage
		if params.System != "" {
			systemPrompt = llm.SystemString(params.System)
			resultMu.Lock()
			result.SystemPrompt = systemPrompt
			resultMu.Unlock()
			return
		}
		if deps.defaultSystem != "" {
			systemPrompt = llm.SystemString(deps.defaultSystem)
			resultMu.Lock()
			result.SystemPrompt = systemPrompt
			resultMu.Unlock()
			return
		}
		if deps.tools == nil {
			return
		}
		tz, _ := prompt.LoadCachedTimezone()
		// Channel feeds the prompt only (runtime line + SupportsRichUI gate).
		// Runs without a DeliveryContext that piggyback on a client session
		// (heartbeat, boot) fall back to the session's channel so their
		// system prompt stays byte-identical to the interactive turns of the
		// same session — one APC prefix family instead of two.
		ch := deliveryChannel(params.Delivery)
		if ch == "" {
			ch = sessionFallbackChannel(params.SessionKey)
		}
		// Build tool defs — filtered if a preset is active.
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		toolDefs := toPromptToolDefs(deps.tools.FilteredDefinitions(allowed))

		// Deferred tool summaries for system prompt listing.
		deferredSummaries := deps.tools.DeferredSummaries()
		var deferredToolInfos []prompt.DeferredToolInfo
		for _, ds := range deferredSummaries {
			// Skip deferred tools not in the allowed preset (if preset is active).
			if _, ok := allowed[ds.Name]; len(allowed) > 0 && !ok {
				continue
			}
			deferredToolInfos = append(deferredToolInfos, prompt.DeferredToolInfo{
				Name:        ds.Name,
				Description: ds.Description,
			})
		}

		// P4: read CompactionFired from session right before assembly so
		// the system prompt's one-time compaction reminder appears from
		// the turn after first compaction onward. Sticky flag — once set
		// it stays set, keeping the dynamic block byte-stable for the
		// trailing message cache markers' prefix matching.
		compactionFired := false
		if deps.sessions != nil {
			if sess := deps.sessions.Get(params.SessionKey); sess != nil {
				compactionFired = sess.CompactionFired
			}
		}

		// Per-topic knowledge: map the forum threadID (from the delivery
		// context) to a topic key, then load <dir>/<key>.md (frozen per
		// session). The content joins the Static cache block; topicCacheKey
		// keys that cache per topic + content hash so topics never collide and
		// edits invalidate. Unmapped/missing → empty (no injection, no cache
		// key change → topic-less Static cache stays shared).
		var topicKnowledge, topicCacheKey, topicKnowledgePath string
		if deps.topicResolver != nil && params.Delivery != nil {
			if key := deps.topicResolver.TopicKey(params.Delivery.ThreadID); key != "" {
				tk := prompt.LoadTopicKnowledge(workspaceDir, deps.topicResolver.Dir(), key, params.SessionKey)
				if tk.Content != "" {
					topicKnowledge = tk.Content
					topicCacheKey = tk.Key + ":" + tk.Hash
					topicKnowledgePath = tk.Path
				}
			}
		}

		// Ambient calendar glance for the dynamic block. The provider freezes
		// it per day, so this is a cheap cache hit on all but the first turn of
		// the day; "" when no calendar source or no upcoming events.
		var calendarGlance string
		if deps.calendarGlanceFn != nil {
			calendarGlance = deps.calendarGlanceFn(ctx, params.SessionKey, tz)
		}

		spp := prompt.SystemPromptParams{
			WorkspaceDir:       workspaceDir,
			ToolDefs:           toolDefs,
			DeferredTools:      deferredToolInfos,
			UserTimezone:       tz,
			ContextFiles:       prompt.LoadContextFiles(workspaceDir, prompt.WithSessionSnapshot(params.SessionKey)),
			RuntimeInfo:        prompt.BuildDefaultRuntimeInfo(params.Model, deps.callbacks.defaultModel),
			Channel:            ch,
			SkillsPrompt:       loadCachedSkillsPrompt(workspaceDir, availableToolNames(deps.tools)),
			ToolPreset:         sessionToolPreset,
			CompactionFired:    compactionFired,
			HindsightEnabled:   deps.hindsightClient != nil,
			CalendarGlance:     calendarGlance,
			TopicKnowledge:     topicKnowledge,
			TopicCacheKey:      topicCacheKey,
			TopicKnowledgePath: topicKnowledgePath,
			SupportsRichUI:     richUIChannel(ch),
		}

		systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp))
		resultMu.Lock()
		result.SystemPrompt = systemPrompt
		resultMu.Unlock()
	})

	prepWg.Wait()
	return result
}

// compactionHooks holds optional callbacks for the STW compaction phase.
// When LLM compaction fires, these hooks provide user-visible feedback
// (status emoji + typing keepalive) so the user knows the system is working.
type compactionHooks struct {
	onStart  func() // called when LLM compaction begins (e.g. set ✍ emoji)
	typingFn func() // sends typing indicator every 5s during compaction
}

// minCompactionBudget is the floor below which an effective context budget is
// treated as a history-suppression sentinel rather than a real budget. Real
// budgets are tens of thousands of tokens (boot passes 30K, defaults are
// 140K+); the only sub-floor caller is the skill-review fork's
// MaxHistoryTokens=1. A single protected turn already exceeds such a budget,
// so compaction cannot succeed by construction.
const minCompactionBudget = 1024

// skipCompactionBudget reports whether the effective context budget is a
// deliberate history-suppression sentinel, in which case Polaris compaction
// is skipped entirely instead of burning every tier and warning each run.
// Zero means "no budget configured" and keeps the legacy behavior.
func skipCompactionBudget(budget int) bool {
	return budget > 0 && budget < minCompactionBudget
}

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
	messages := prep.Messages

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		// Copy to avoid aliasing the caller's backing array. Without the copy,
		// append may write into shared capacity, corrupting the original slice.
		messages = append([]llm.Message(nil), params.PrebuiltMessages...)
		// When the caller also supplies a Message, append it so the LLM sees
		// it without re-loading the entire transcript.
		if params.Message != "" && len(params.Attachments) == 0 {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	}

	// Build or augment user message with attachments.
	if len(messages) == 0 && params.Message != "" {
		// No history — build the user message from scratch.
		if len(params.Attachments) > 0 {
			blocks := buildAttachmentBlocks(params.Message, params.Attachments)
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	} else if len(messages) > 0 && len(params.Attachments) > 0 {
		// History exists but current message has attachments — replace the
		// last user message (which was persisted as text-only) with a
		// multimodal version that includes the image/video content blocks.
		messages = appendAttachmentsToHistory(messages, params.Message, params.Attachments)
	}

	// Model marked non-vision (provider config `vision: false`): replace image
	// blocks with text stubs up front instead of letting the provider reject
	// the request. Only fires on an explicit override — unknown models are
	// assumed vision-capable.
	if modelCapability(deps, mr.providerID, mr.model).NoVision {
		messages = compact.StripImageBlocks(messages)
	}

	// Polaris compaction: tiered context compression.
	// Applied after message assembly, before prompt finalization.
	// STW (Stop-the-World): when LLM compaction fires, the user sees a
	// ✍ status emoji and typing keepalive until compaction completes.
	// No LLM call is made until context is compressed — incoming messages
	// are already queued by PendingQueue during the active run.
	if len(messages) > 0 {
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

		polarisCtx, polarisCancel := context.WithTimeout(ctx, 2*time.Minute)
		var summarizer compact.Summarizer
		if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
			summarizer = &localAISummarizer{}
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
				if hooks.onStart != nil {
					hooks.onStart()
				}
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
			if deps.embeddingClient != nil {
				engine.SetEmbedder(deps.embeddingClient)
			}
			engine.SetAnchorKeywords(buildAnchorKeywords(deps.wikiStore))
			engine.SetLearnedGuidelines(buildLearnedGuidelines())
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
			cfg.Embedder = deps.embeddingClient
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

// localAISummarizer adapts pilot.CallLocalLLM to the compaction.Summarizer interface.
type localAISummarizer struct{}

func (s *localAISummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	return pilot.CallAnalysisLLM(ctx, system, conversation, maxOutputTokens)
}

// formatToolHist renders a tool-count histogram as "name:count,name:count" in
// descending count order so the busiest tool surfaces first in the log line.
// Returns "" for an empty map — slog will drop empty string values cleanly.
