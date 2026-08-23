// run_prepare.go — context/prompt preparation stages of the agent run:
// prepareContextAndPrompt (parallel recall+history+prompt), assembleMessages
// (compaction tiers + budget enforcement), finalizePrompt, and the local-AI
// summarizer they share. Called sequentially from executeAgentRun (run_exec.go).
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

type prepResult struct {
	Messages     []llm.Message
	SystemPrompt json.RawMessage
	RecallMemory string
	Tier1Wiki    string
	ContextErr   error
	// ContextFiles and TopicKnowledge are the session-frozen system-prompt
	// inputs captured from the sysprompt goroutine so they can be persisted
	// alongside Tier1Wiki (prompt_snapshot_persist.go). Nil on explicit-System
	// runs (subagents) that bypass prompt assembly.
	ContextFiles   []prompt.ContextFile
	TopicKnowledge *prompt.TopicKnowledge
}

// prepareContextAndPrompt runs wiki injection, context assembly, and system prompt
// build in parallel. Returns the combined results.
func prepareContextAndPrompt(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	workspaceDir string,
	sessionToolPreset string,
	logger *slog.Logger,
) prepResult {
	var result prepResult
	var resultMu sync.Mutex
	var prepWg sync.WaitGroup
	promptSnapshotGeneration := currentPromptSnapshotGeneration()

	// Tier-1 wiki auto-injection (parallel). Frozen per session (tier1_cache.go):
	// FormatTier1 reads the live store, and mid-session wiki writes would
	// otherwise shift the system-prompt tail every few turns — invalidating
	// the vLLM APC prefix for the tool schemas + entire history.
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-tier1-wiki", func() {
		defer prepWg.Done()
		tier1 := buildTier1WikiSnapshot(params, deps)
		resultMu.Lock()
		result.Tier1Wiki = tier1
		resultMu.Unlock()
	})

	// Recall preflight (parallel): inject focused memory before the LLM call —
	// gates, caching, and source fan-out live in buildRecallSnapshot.
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-recall", func() {
		defer prepWg.Done()
		recall := buildRecallSnapshot(ctx, params, deps, logger)
		resultMu.Lock()
		result.RecallMemory = recall
		resultMu.Unlock()
	})

	// Context assembly (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-context", func() {
		defer prepWg.Done()
		messages, contextErr := loadTurnContextMessages(params, deps, logger)
		resultMu.Lock()
		result.Messages = messages
		result.ContextErr = contextErr
		resultMu.Unlock()
	})

	// System prompt build (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-sysprompt", func() {
		defer prepWg.Done()
		systemPrompt, ctxFiles, frozenTopic := buildTurnSystemPrompt(ctx, params, deps, workspaceDir, sessionToolPreset)
		resultMu.Lock()
		result.SystemPrompt = systemPrompt
		result.ContextFiles = ctxFiles
		result.TopicKnowledge = frozenTopic
		resultMu.Unlock()
	})

	prepWg.Wait()

	// Persist this session's frozen system-prompt inputs so the next gateway
	// restart restores byte-identical bytes — preserving the vLLM APC prefix for
	// this session's tool schemas + full history instead of forcing a re-prefill.
	// First-write-wins and a no-op once a session's fields are present, so the
	// common path costs only a lock + map lookup. Reads after Wait() are safe:
	// the WaitGroup is a barrier, so the goroutines' writes are visible here.
	// See prompt_snapshot_persist.go.
	recordPromptSnapshot(params.SessionKey, result.Tier1Wiki, result.ContextFiles, result.TopicKnowledge, promptSnapshotGeneration)

	return result
}

// buildTier1WikiSnapshot returns the session-frozen tier-1 wiki block ("" for
// explicit-System runs, which own their prompt).
func buildTier1WikiSnapshot(params RunParams, deps runDeps) string {
	if deps.disableTier1Wiki {
		return ""
	}
	var tier1 string
	// Explicit-System runs (subagents, skill-review forks) own their prompt
	// end to end — finalizePrompt would otherwise append always-on wiki
	// memory on top of a deliberately lean caller prompt (review-sweep
	// finding on #3103: the mini review prompt still carried tier-1 wiki).
	if deps.memory.Wiki != nil && params.System == "" {
		cached, ok, generation := cachedTier1WikiWithGeneration(params.SessionKey)
		if ok {
			tier1 = cached
		} else {
			tier1 = tooldeps.FormatWikiTier1(deps.memory.Wiki)
			storeTier1WikiIfGeneration(params.SessionKey, tier1, generation)
		}
	}
	return tier1
}

// buildRecallSnapshot runs the recall preflight for the turn: profile/toggle
// gates, per-cue caching, and the multi-source search. Returns the wire-ready
// recall block ("" when gated or no evidence).
func buildRecallSnapshot(ctx context.Context, params RunParams, deps runDeps, logger *slog.Logger) string {
	// Ephemeral turns (autonomous heartbeat self-triggers) never run
	// recall — there is no real user message to recall against. SkipRecall
	// is the user's "focused chat / memory off" toggle: skip the whole
	// preflight so a general question pays no search latency and pulls no
	// unrelated work memories.
	if params.EphemeralUser || params.SkipRecall {
		return ""
	}
	// A notebook with real sources bound to this session is the explicit
	// scope, so suppress broad recall — the pinned sources are tail-injected
	// as grounding instead (run_exec.go), and running whole-corpus recall
	// alongside would dilute that focus and compete for the input budget. An
	// empty/missing notebook does NOT suppress recall, so a contentless bound
	// turn is never left with neither (the gates stay symmetric). The wiki/
	// recall TOOLS stay available if the model needs wider memory.
	if _, _, ok := activeGroundingNotebook(deps, params.SessionKey); ok {
		return ""
	}
	fingerprint := chatrecall.CueFingerprint(params.Message)
	hasCue := fingerprint != ""
	// A rewritten elliptical turn depends on the immediately preceding user
	// message. The raw cue alone is not a safe cache key: repeating "그거 뭐였지?"
	// after Alpha then Beta must not reuse Alpha's fact snapshot in Beta context.
	cacheableCue := hasCue && !chatrecall.NeedsContextRewrite(params.Message)
	var recallCacheGeneration uint64
	// Hermes-style auto_recall: run the preflight every turn, not just cue turns.
	// recall.Build searches wiki/diary/polaris/transcript and returns
	// "" silently when there's no evidence, so non-cue turns add latency but no noise.
	if hasCue && !deps.briefcaseMode {
		if cacheableCue {
			cached, ok, generation := chatrecall.CachedSnapshotWithGeneration(params.SessionKey, fingerprint)
			recallCacheGeneration = generation
			if ok {
				return cached
			}
		}
		// Explicit recall: surface the recalling phase so the user sees the
		// wiki/diary/transcript search. Silent auto-recall on no-cue turns
		// stays invisible.
		emitPhase(deps, params, "recalling", time.Now())
	}
	recallMemory, recallTruncated := chatrecall.Build(
		ctx,
		chatrecall.Params{
			SessionKey:    params.SessionKey,
			Message:       params.Message,
			EphemeralUser: params.EphemeralUser,
			SkipRecall:    params.SkipRecall,
		},
		chatrecall.Deps{
			Wiki:         deps.memory.Wiki,
			Transcript:   deps.transcript,
			FileRecall:   deps.memory.FileRecall,
			Org:          deps.memory.Org,
			Briefcase:    deps.briefcaseMode,
			StrictErrors: deps.strictErrors,
			Now:          deps.now,
		},
		logger,
	)
	if !deps.briefcaseMode && cacheableCue && chatrecall.ShouldFreeze(hasCue, recallTruncated, recallMemory) {
		chatrecall.StoreSnapshotIfGeneration(params.SessionKey, fingerprint, recallMemory, recallCacheGeneration)
	}
	return recallMemory
}

// loadTurnContextMessages assembles the transcript-backed message history for
// the turn (polaris bridge path); (nil, nil) when no bridge is wired.
func loadTurnContextMessages(params RunParams, deps runDeps, logger *slog.Logger) ([]llm.Message, error) {
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
	return messages, contextErr
}

// buildTurnSystemPrompt assembles the system prompt blocks plus the
// session-frozen inputs that must be persisted alongside them
// (prompt_snapshot_persist.go). Explicit-System runs and default-system
// deployments short-circuit with nil frozen inputs.
func buildTurnSystemPrompt(ctx context.Context, params RunParams, deps runDeps, workspaceDir, sessionToolPreset string) (json.RawMessage, []prompt.ContextFile, *prompt.TopicKnowledge) {
	if params.System != "" {
		return json.RawMessage(llm.SystemString(params.System).Bytes()), nil, nil
	}
	if deps.defaultSystem != "" {
		return json.RawMessage(llm.SystemString(deps.defaultSystem).Bytes()), nil, nil
	}
	if deps.tools == nil {
		return nil, nil, nil
	}
	tz, _ := prompt.LoadCachedTimezone()
	if deps.semanticTimezone != "" {
		tz = deps.semanticTimezone
	}
	// Channel feeds the prompt only (the runtime line).
	// Runs without a DeliveryContext that piggyback on a client session
	// (heartbeat, boot) fall back to the session's channel so their
	// system prompt stays byte-identical to the interactive turns of the
	// same session — one APC prefix family instead of two.
	ch := deliveryChannel(params.Delivery)
	if ch == "" {
		ch = sessionFallbackChannel(params.SessionKey)
	}
	// Build tool defs — filtered if a preset is active.
	allowed := toolwire.AllowedTools(sessionToolPreset)
	toolDefs := toPromptToolDefs(deps.tools.FilteredDefinitions(allowed))

	// Deferred tool summaries for system prompt listing.
	preloaded := make(map[string]struct{})
	for _, n := range toolwire.PreloadedDeferredTools(sessionToolPreset) {
		preloaded[n] = struct{}{}
	}
	for _, n := range filterPreloadedDeferredToolNames(deps.tools, params.InitialDeferredTools, sessionToolPreset) {
		preloaded[n] = struct{}{}
	}
	deferredSummaries := deps.tools.DeferredSummaries()
	var deferredToolInfos []prompt.DeferredToolInfo
	for _, ds := range deferredSummaries {
		// Skip deferred tools not in the allowed preset (if preset is active).
		if _, ok := allowed[ds.Name]; len(allowed) > 0 && !ok {
			continue
		}
		// Skip tools pre-loaded as active for this preset — they're directly
		// callable, so listing them as deferred would be wrong (and tell the
		// model to fetch_tools something it already has).
		if _, ok := preloaded[ds.Name]; ok {
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
	var frozenTopic *prompt.TopicKnowledge
	if deps.ambient.TopicResolver != nil && params.Delivery != nil && !deps.briefcaseMode {
		if key := deps.ambient.TopicResolver.TopicKey(params.Delivery.ThreadID); key != "" {
			tk := prompt.LoadTopicKnowledge(workspaceDir, deps.ambient.TopicResolver.Dir(), key, params.SessionKey)
			if tk.Content != "" {
				topicKnowledge = tk.Content
				topicCacheKey = tk.Key + ":" + tk.Hash
				topicKnowledgePath = tk.Path
				tkCopy := tk
				frozenTopic = &tkCopy
			}
		}
	}

	// Ambient calendar glance for the dynamic block. The provider freezes
	// it per day, so this is a cheap cache hit on all but the first turn of
	// the day; "" when no calendar source or no upcoming events.
	var calendarGlance string
	if deps.ambient.CalendarGlance != nil && !deps.briefcaseMode {
		calendarGlance = deps.ambient.CalendarGlance(ctx, params.SessionKey, tz)
	}

	// Ambient goal glance for the dynamic block: this session's active
	// standing goal, read live from the process store. "" when no active
	// goal or goals are not wired.
	var goalGlance string
	if deps.ambient.GoalGlance != nil && !deps.briefcaseMode {
		goalGlance = deps.ambient.GoalGlance(ctx, params.SessionKey)
	}

	var ctxFiles []prompt.ContextFile
	if !deps.briefcaseMode {
		ctxFiles = loadFactAwareContextFiles(workspaceDir, params.SessionKey)
	}

	// Operator-edited 업무 persona (Settings prompt corner). "" override → default
	// persona renders, byte-identical to before. PersonaCacheKey (content hash)
	// keys the Static cache per persona so an edit invalidates only its own entry.
	var personaText, personaCacheKey string
	if deps.ambient.PersonaOverride != nil && !deps.briefcaseMode {
		if ov := strings.TrimSpace(deps.ambient.PersonaOverride()); ov != "" {
			personaText = ov
			personaCacheKey = prompt.PersonaCacheKeyFor(ov)
		}
	}

	skillsPrompt := ""
	if !deps.briefcaseMode {
		skillsPrompt = loadCachedSkillsPrompt(workspaceDir, availableToolNames(deps.tools))
	}

	promptWorkspaceDir := workspaceDir
	if deps.promptWorkspaceDir != "" {
		promptWorkspaceDir = deps.promptWorkspaceDir
	}
	runtimeInfo := prompt.BuildDefaultRuntimeInfo(params.Model, deps.callbacks.defaultModel)
	if deps.briefcaseMode {
		runtimeInfo = &prompt.RuntimeInfo{
			AgentID: "briefcase", Host: "briefcase", OS: "isolated", Arch: "v1",
			Model: params.Model, DefaultModel: deps.callbacks.defaultModel,
		}
	}

	spp := prompt.SystemPromptParams{
		WorkspaceDir:       promptWorkspaceDir,
		ToolDefs:           toolDefs,
		DeferredTools:      deferredToolInfos,
		UserTimezone:       tz,
		ContextFiles:       ctxFiles,
		RuntimeInfo:        runtimeInfo,
		Channel:            ch,
		SkillsPrompt:       skillsPrompt,
		ToolPreset:         sessionToolPreset,
		CompactionFired:    compactionFired,
		Briefcase:          deps.briefcaseMode,
		DisableSkills:      deps.briefcaseMode,
		CalendarGlance:     calendarGlance,
		GoalGlance:         goalGlance,
		TopicKnowledge:     topicKnowledge,
		TopicCacheKey:      topicCacheKey,
		TopicKnowledgePath: topicKnowledgePath,
		PersonaText:        personaText,
		PersonaCacheKey:    personaCacheKey,
		Now:                deps.now(),
	}

	return json.RawMessage(llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp)).Bytes()), ctxFiles, frozenTopic
}

// compactionHooks holds optional callbacks for the STW compaction phase.
// When LLM compaction fires, these hooks provide user-visible feedback
// (typing keepalive) so the user knows the system is working.
type compactionHooks struct {
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

// formatToolHist renders a tool-count histogram as "name:count,name:count" in
// descending count order so the busiest tool surfaces first in the log line.
// Returns "" for an empty map — slog will drop empty string values cleanly.
// (Implementation lives in run_exec.go; this package-level doc comment covers
// the shared helper.)
