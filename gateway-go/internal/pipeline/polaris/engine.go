package polaris

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// summaryPrefix detects legacy already-summarized content so Polaris can skip
// re-summarization. New summaries use compaction.FormatContextFence instead.
const summaryPrefix = "[이전 대화 요약"

const (
	// bootstrapRawThreshold: if older messages are below this, inject raw (no LLM).
	bootstrapRawThreshold = 50_000
)

// Engine orchestrates Polaris compaction with DAG persistence and condensation.
// Long-lived: stored on Bridge, shared across runs for the same session.
type Engine struct {
	store             *Store
	logger            *slog.Logger
	cfg               Config
	circuit           *CircuitBreaker
	embedder          compact.Embedder // optional; BGE-M3 for MMR compaction fallback
	anchorMu          sync.RWMutex
	anchorKeywords    []string // wiki Tier1 page titles to preserve through summarization
	learnedGuidelines []string // ACON-style preservation rules distilled from past compaction misses

	// compacting single-flights background compaction per session: a session
	// key is present while CompactInBackground is running for it. Overlapping
	// background passes would do redundant LLM work and could persist duplicate
	// summary nodes, so a second launch is a no-op until the first completes.
	// Independent of anchorMu (a sync.Map needs no external lock).
	compacting sync.Map // sessionKey → struct{}
}

// NewEngine creates a Polaris engine backed by the given store.
func NewEngine(store *Store, logger *slog.Logger, cfg Config) *Engine {
	return &Engine{
		store:   store,
		logger:  logger,
		cfg:     cfg,
		circuit: NewCircuitBreaker(3),
	}
}

// SetEmbedder sets the optional embedding client for MMR compaction fallback.
func (e *Engine) SetEmbedder(emb compact.Embedder) { e.embedder = emb }

// SetAnchorKeywords sets wiki Tier1 page titles whose facts MUST be preserved
// through summarization. Safe to call concurrently — the keyword list is
// snapshotted into the per-call compaction config.
func (e *Engine) SetAnchorKeywords(keywords []string) {
	e.anchorMu.Lock()
	defer e.anchorMu.Unlock()
	e.anchorKeywords = append(e.anchorKeywords[:0], keywords...)
}

// SetLearnedGuidelines sets ACON-style preservation rules distilled from past
// compaction misses. Snapshotted into the per-call config like anchors; shares
// anchorMu since both are externally-set preservation hints read per-compact.
func (e *Engine) SetLearnedGuidelines(guidelines []string) {
	e.anchorMu.Lock()
	defer e.anchorMu.Unlock()
	e.learnedGuidelines = append(e.learnedGuidelines[:0], guidelines...)
}

// CompactAndPersist runs Polaris compaction and persists any LLM summary
// into the DAG as a leaf node. Summary messages already injected by
// AssembleContext (or bootstrap) are protected from re-summarization, but the
// raw remainder still goes through the full tier chain.
//
// Bootstrap: when no summaries exist in the DAG, older messages that were
// dropped by freshTailCount during assembly are recovered here. If the older
// messages are < 50K tokens they are injected raw; otherwise they are
// LLM-compacted and the summary is persisted to the DAG.
func (e *Engine) CompactAndPersist(
	ctx context.Context,
	sessionKey string,
	messages []llm.Message,
	summarizer compact.Summarizer,
	contextBudget int,
) ([]llm.Message, compact.Result) {
	polarisCfg := compact.NewConfig(contextBudget)
	polarisCfg.Embedder = e.embedder
	e.anchorMu.RLock()
	polarisCfg.AnchorKeywords = append([]string(nil), e.anchorKeywords...)
	polarisCfg.LearnedGuidelines = append([]string(nil), e.learnedGuidelines...)
	e.anchorMu.RUnlock()

	// Bootstrap: recover older messages dropped by freshTailCount.
	// Shares polarisCfg so the bootstrap summarization path uses the same
	// output budget / chunking / parallelism as regular LLM compaction.
	messages = e.bootstrapIfNeeded(ctx, sessionKey, messages, summarizer, polarisCfg)

	// Injected summary fences (from AssembleContext or bootstrap) must not be
	// re-summarized — but they must not disable the LLM tier either. A
	// previous blanket SkipLLMCompaction here meant that once a single DAG
	// summary existed, the uncovered raw tail could never be summarized again
	// and grew without bound (client:main reached 474 raw messages / 318K
	// tokens while the blunt safety trim did all the cutting, every turn).
	// Instead: split the fences out, run the tier chain on the raw remainder
	// with a correspondingly reduced budget, and re-attach the fences after.
	fences, rest := splitSummaryFences(messages)
	innerCfg := polarisCfg
	fenceTokens := 0
	if len(fences) > 0 {
		fenceTokens = compact.EstimateMessagesTokens(fences)
		if innerBudget := contextBudget - fenceTokens; innerBudget > 0 {
			innerCfg.ContextBudget = innerBudget
		} else {
			// Degenerate: the fences alone exceed the budget — no room for raw
			// context, so another summary buys nothing. Cheap tiers still run;
			// the safety trim below handles the rest.
			innerCfg.SkipLLMCompaction = true
		}
	}

	// Wrap summarizer to capture the summary text for DAG persistence.
	var capturedSummary string
	var wrappedSummarizer compact.Summarizer
	if summarizer != nil {
		wrappedSummarizer = &capturingSummarizer{
			inner:    summarizer,
			captured: &capturedSummary,
		}
	}

	// Run Polaris 3-tier compaction on the raw remainder.
	compacted, result := compact.Compact(ctx, innerCfg, rest, wrappedSummarizer, e.logger)

	// Persist the summary to the DAG. Prefer Result.Summary — when the
	// chunked path ran it is the joined text of all chunk summaries, while
	// capturedSummary is whichever single Summarize() call finished last (one
	// chunk's slice of the range; persisting it as covering the whole range
	// would silently drop the other chunks' facts). The captured value remains
	// the fallback for the emergency tier, which does not surface its summary
	// in Result.Summary.
	summaryToPersist := result.Summary
	if summaryToPersist == "" {
		summaryToPersist = capturedSummary
	}
	if summaryToPersist != "" && (result.LLMCompacted || result.EmergencyEvicted > 0) {
		// Count only the inner compacted slice: persistSummary derives the
		// covered range from how many real transcript messages were preserved,
		// and the fences are not transcript messages.
		e.persistSummary(sessionKey, summaryToPersist, len(compacted))
	}

	// Re-attach the protected fences and fold their tokens back into the
	// result so callers (budget warnings, tier logs) see whole-context totals.
	if len(fences) > 0 {
		full := make([]llm.Message, 0, len(fences)+len(compacted))
		full = append(full, fences...)
		full = append(full, compacted...)
		compacted = full
		result.TokensBefore += fenceTokens
		result.TokensAfter += fenceTokens
	}

	// Safety-net: if compaction could not bring the context within budget
	// (e.g. an uncovered raw backlog is being digested a few chunks per pass),
	// drop oldest raw messages as a last resort — keeping summary fences, the
	// densest context, and never leaving an orphaned tool pair. The result's
	// TokensAfter is updated so the caller's "failed to reduce below budget"
	// check fires only when even this trim could not get under budget.
	if contextBudget > 0 && compact.EstimateMessagesTokens(compacted) > contextBudget {
		before := compact.EstimateMessagesTokens(compacted)
		compacted = trimWithFenceProtection(compacted, contextBudget)
		after := compact.EstimateMessagesTokens(compacted)
		result.TokensAfter = after
		e.logger.Warn("polaris: post-compaction safety trim fired",
			"tokensBefore", before, "tokensAfter", after, "budget", contextBudget)
	}

	return compacted, result
}

// HasSummaries reports whether the session has at least one persisted summary.
// When false the session is still in the bootstrap stage: AssembleContext
// returns a fresh-tail-truncated prefix and CompactAndPersist recovers the
// dropped older messages, so compaction must stay on the critical path. Once a
// summary exists every message after coverage is assembled raw, which is the
// precondition for running a turn raw and compacting in the background instead.
func (e *Engine) HasSummaries(sessionKey string) bool {
	cov, err := e.store.LatestSummaryCoverage(sessionKey)
	return err == nil && cov >= 0
}

// CompactionInFlight reports whether a background compaction is running for the
// session. Callers use it only as a hint; CompactInBackground is itself
// single-flighted, so a racing launch is always a safe no-op.
func (e *Engine) CompactionInFlight(sessionKey string) bool {
	_, ok := e.compacting.Load(sessionKey)
	return ok
}

// tryAcquireCompaction marks a session as compacting and reports whether the
// caller won the single-flight (false = another pass already holds it).
func (e *Engine) tryAcquireCompaction(sessionKey string) bool {
	_, loaded := e.compacting.LoadOrStore(sessionKey, struct{}{})
	return !loaded
}

func (e *Engine) releaseCompaction(sessionKey string) { e.compacting.Delete(sessionKey) }

// CompactInBackground summarizes a session's uncovered tail OFF the critical
// path and persists the summary to the DAG, so a later turn assembles an
// already-compacted context without a synchronous stop-the-world (STW)
// summarization. The current turn runs on the raw context (cheap and APC-cached
// on the local-vLLM path, where decode is flat well past the configured
// budget); the next turn picks up the persisted summary.
//
// Safety:
//   - Single-flighted per session — a no-op if a pass is already running.
//   - Race-free coverage: the store's max message index is pinned at launch and
//     the persisted summary covers only [coverage+1, coverage+covered] within
//     that pinned range. Messages appended while the pass runs (the current
//     turn's own tool results, or a following turn) keep indices past the pin,
//     stay uncovered, and are never silently dropped.
//   - Tool-pair safe: the covered boundary is snapped back past any trailing
//     tool_result so the next assembly's recent window never begins with an
//     orphan tool_result whose tool_use was summarized (Anthropic 400 / wedge).
//   - Lifetime: derived from parentCtx (the server shutdown context) so it
//     outlives the request turn but is cancelled on graceful shutdown, bounded
//     by a 5-minute timeout, and wrapped in safego so a panic cannot take down
//     the process.
//
// summarizer must be non-nil. embedder/anchors/guidelines are snapshotted by
// the caller (mirroring CompactAndPersist's per-call config) and captured in
// the closure, so no shared engine state is read concurrently.
func (e *Engine) CompactInBackground(
	parentCtx context.Context,
	sessionKey string,
	summarizer compact.Summarizer,
	contextBudget int,
	embedder compact.Embedder,
	anchors, guidelines []string,
) {
	if summarizer == nil || contextBudget <= 0 {
		return
	}
	if !e.tryAcquireCompaction(sessionKey) {
		return // another pass already running for this session
	}
	// The single-flight is released by the spawned goroutine; if we bail out
	// before spawning it, this defer releases it instead (exactly one release).
	spawned := false
	defer func() {
		if !spawned {
			e.releaseCompaction(sessionKey)
		}
	}()

	// Pin a consistent store snapshot BEFORE spawning so the covered range
	// cannot drift as new messages append.
	coverage, _ := e.store.LatestSummaryCoverage(sessionKey)
	pinnedMax, err := e.store.MaxMsgIndex(sessionKey)
	if err != nil || pinnedMax <= coverage {
		return // nothing uncovered to compact
	}
	rawChat, err := e.store.LoadMessages(sessionKey, coverage+1, pinnedMax)
	if err != nil || len(rawChat) == 0 {
		return
	}
	raw := chatToLLM(rawChat)
	// Only worth a background LLM pass when the uncovered tail itself is over
	// the LLM threshold — the same gate the synchronous tier uses, measured on
	// the tail. Below it, summarizing buys little; a large existing summary set
	// is the condense path's job and is handled by the synchronous hard-ceiling
	// backstop if it ever pushes the context to the window.
	if compact.EstimateMessagesTokens(raw) <= int(float64(contextBudget)*compact.DefaultLLMThresholdPct) {
		return
	}

	cfg := compact.NewConfig(contextBudget)
	cfg.Embedder = embedder
	cfg.AnchorKeywords = anchors
	cfg.LearnedGuidelines = guidelines

	if parentCtx == nil {
		parentCtx = context.Background()
	}

	spawned = true // the goroutine now owns the single-flight release
	safego.GoWithSlog(e.logger, "polaris-bg-compact", func() {
		defer e.releaseCompaction(sessionKey)
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
		defer cancel()

		compacted, summary, ok := compact.LLMCompact(ctx, cfg, raw, summarizer, e.logger)
		if !ok || summary == "" {
			return
		}
		// covered = how many leading raw messages the summary covers. Mirrors
		// the synchronous persistSummary math (preserved = compacted - the one
		// summary message) but against the PINNED tail rather than live state.
		preserved := len(compacted) - 1
		if preserved < 0 {
			preserved = 0
		}
		covered := len(raw) - preserved
		covered = safeCoverageCount(raw, covered)
		if covered <= 0 {
			return
		}
		msgStart := coverage + 1
		msgEnd := coverage + covered
		node := SummaryNode{
			SessionKey: sessionKey,
			Level:      1,
			Content:    summary,
			TokenEst:   compact.EstimateTokens(summary),
			CreatedAt:  time.Now().UnixMilli(),
			MsgStart:   msgStart,
			MsgEnd:     msgEnd,
		}
		id, ierr := e.store.InsertSummary(node)
		if ierr != nil {
			e.logger.Warn("polaris: background summary persist failed", "session", sessionKey, "error", ierr)
			return
		}
		e.logger.Info("polaris: background compaction persisted summary",
			"id", id, "session", sessionKey, "range", [2]int{msgStart, msgEnd},
			"coveredMessages", covered, "summaryTokens", node.TokenEst)

		// Mirror the synchronous path: merge leaves into higher-level nodes so
		// summaries do not accumulate unbounded (the sync Condense goroutine in
		// run_prepare does not fire for deferred turns).
		if cErr := e.Condense(ctx, sessionKey, summarizer); cErr != nil {
			e.logger.Warn("polaris: background condense failed", "session", sessionKey, "error", cErr)
		}
	})
}

// safeCoverageCount snaps a covered-message count back past any trailing
// tool_result so the boundary between the summarized prefix raw[:covered] and
// the uncovered remainder raw[covered:] never splits a tool_use↔tool_result
// pair. The chunk splitter (splitIntoChunks) is token-aligned, not pair-aligned,
// so a bounded-digestion boundary can otherwise leave raw[covered] as a
// tool_result whose tool_use was summarized — which the next assembly would load
// as an orphan and Anthropic would reject with a 400. Backing the boundary off
// keeps both halves of the pair on the uncovered side (a few raw messages may
// then also be described by the summary; harmless duplication, never an orphan).
func safeCoverageCount(raw []llm.Message, covered int) int {
	if covered > len(raw) {
		covered = len(raw)
	}
	for covered > 0 && covered < len(raw) && compact.IsToolResultMessage(raw[covered].Content) {
		covered--
	}
	return covered
}

// splitSummaryFences partitions messages into injected summary-fence messages
// (from AssembleContext, bootstrap, or legacy compaction) and the raw
// remainder, preserving the relative order of both groups. In an assembled
// context the fences are a strict prefix, so this is effectively a prefix
// split that also tolerates strays.
func splitSummaryFences(messages []llm.Message) (fences, rest []llm.Message) {
	for _, m := range messages {
		if isSummaryFenceMessage(m) {
			fences = append(fences, m)
		} else {
			rest = append(rest, m)
		}
	}
	return fences, rest
}

// isSummaryFenceMessage reports whether a message is injected summary context
// rather than a real transcript message.
func isSummaryFenceMessage(m llm.Message) bool {
	if m.Role != "user" {
		return false
	}
	var text string
	if json.Unmarshal(m.Content, &text) != nil {
		return false
	}
	return strings.HasPrefix(text, summaryPrefix) || compact.IsContextFenceText(text)
}

// trimWithFenceProtection drops oldest raw messages until the context fits the
// budget while keeping summary fences (compressed context is the densest
// information per token). Only when the fences alone exceed the budget does it
// fall back to trimming across everything. Either way the result is re-balanced
// so a tool_result whose tool_use was trimmed away cannot survive as an orphan
// (Anthropic rejects those with a 400).
func trimWithFenceProtection(msgs []llm.Message, budget int) []llm.Message {
	fences, rest := splitSummaryFences(msgs)
	rawBudget := budget - compact.EstimateMessagesTokens(fences)
	if rawBudget > 0 && len(rest) > 0 {
		rest = trimLLMToTokenBudget(rest, rawBudget)
		out := make([]llm.Message, 0, len(fences)+len(rest))
		out = append(out, fences...)
		out = append(out, rest...)
		if compact.EstimateMessagesTokens(out) <= budget {
			return compact.BalanceToolBlocks(out)
		}
	}
	// Degenerate: fences alone exceed the budget (or trimming raw was not
	// enough) — trim across the full slice, oldest first.
	return compact.BalanceToolBlocks(trimLLMToTokenBudget(msgs, budget))
}

// persistSummary stores a compaction summary as a leaf node in the DAG.
func (e *Engine) persistSummary(sessionKey, summary string, compactedCount int) {
	coverage, err := e.store.LatestSummaryCoverage(sessionKey)
	if err != nil {
		e.logger.Warn("polaris: failed to query coverage", "error", err)
		coverage = -1
	}

	msgStart := coverage + 1

	maxIdx, err := e.store.MaxMsgIndex(sessionKey)
	if err != nil || maxIdx < 0 {
		e.logger.Warn("polaris: failed to query max index, skipping persist", "error", err)
		return
	}

	preserved := compactedCount - 1
	if preserved < 0 {
		preserved = 0
	}
	msgEnd := maxIdx - preserved
	if msgEnd < msgStart {
		return
	}

	node := SummaryNode{
		SessionKey: sessionKey,
		Level:      1,
		Content:    summary,
		TokenEst:   compact.EstimateTokens(summary),
		CreatedAt:  time.Now().UnixMilli(),
		MsgStart:   msgStart,
		MsgEnd:     msgEnd,
	}

	id, err := e.store.InsertSummary(node)
	if err != nil {
		e.logger.Warn("polaris: failed to persist summary node", "error", err)
		return
	}
	e.logger.Info("polaris: persisted summary node",
		"id", id, "session", sessionKey,
		"range", [2]int{msgStart, msgEnd}, "tokens", node.TokenEst)
}

// bootstrapIfNeeded recovers older messages that were dropped by freshTailCount
// during assembly when no DAG summaries exist yet. This breaks the catch-22
// where truncated context never triggers LLM compaction.
//
// Policy:
//   - older < 50K tokens → inject raw messages (no LLM call)
//   - older ≥ 50K tokens → LLM compact + persist to DAG
func (e *Engine) bootstrapIfNeeded(
	ctx context.Context,
	sessionKey string,
	messages []llm.Message,
	summarizer compact.Summarizer,
	cfg compact.Config,
) []llm.Message {
	coverage, _ := e.store.LatestSummaryCoverage(sessionKey)
	if coverage >= 0 {
		return messages // summaries already exist in DAG
	}

	maxIdx, err := e.store.MaxMsgIndex(sessionKey)
	if err != nil || maxIdx < 0 {
		return messages
	}

	totalMessages := maxIdx + 1
	if totalMessages <= len(messages) {
		return messages // no dropped messages
	}

	// Older messages: everything before the fresh tail.
	olderEnd := maxIdx - len(messages) // inclusive end index
	if olderEnd < 0 {
		return messages
	}

	olderChatMsgs, err := e.store.LoadMessages(sessionKey, 0, olderEnd)
	if err != nil || len(olderChatMsgs) == 0 {
		return messages
	}

	olderLLM := chatToLLM(olderChatMsgs)
	olderTokens := compact.EstimateMessagesTokens(olderLLM)

	if olderTokens < bootstrapRawThreshold {
		// Under threshold: inject raw older messages before fresh tail.
		enriched := make([]llm.Message, 0, len(olderLLM)+len(messages))
		enriched = append(enriched, olderLLM...)
		enriched = append(enriched, messages...)
		e.logger.Info("polaris: bootstrap raw inject",
			"session", sessionKey,
			"olderMessages", len(olderLLM),
			"olderTokens", olderTokens)
		return enriched
	}

	// Over threshold: LLM compact older messages.
	if summarizer == nil {
		return messages
	}

	summary, covered := compact.BootstrapCompact(ctx, cfg, olderLLM, summarizer, e.logger)
	if summary == "" || covered <= 0 {
		return messages
	}
	// A huge backlog is digested in bounded chunk batches, so the summary may
	// cover only a prefix of the older messages. Persist exactly that range:
	// olderLLM[i] holds store msg index i (LoadMessages from 0, append-only
	// sequential indices), so the covered range is [0, covered-1]. Messages
	// past coveredEnd stay uncovered; once coverage exists the next assembly
	// loads them as raw recent and regular compaction digests them.
	coveredEnd := covered - 1
	if coveredEnd > olderEnd {
		coveredEnd = olderEnd
	}

	// Inject summary message at the front of context.
	summaryText := compact.FormatContextFence(
		"polaris-bootstrap",
		"conversation-summary",
		fmt.Sprintf("이전 대화 요약 (메시지 0-%d)", coveredEnd),
		summary,
	)
	summaryMsg := llm.NewTextMessage("user", summaryText)
	enriched := make([]llm.Message, 0, 1+len(messages))
	enriched = append(enriched, summaryMsg)
	enriched = append(enriched, messages...)

	// Persist to DAG so future assembly picks it up directly.
	node := SummaryNode{
		SessionKey: sessionKey,
		Level:      1,
		Content:    summary,
		TokenEst:   compact.EstimateTokens(summary),
		CreatedAt:  time.Now().UnixMilli(),
		MsgStart:   0,
		MsgEnd:     coveredEnd,
	}
	id, err := e.store.InsertSummary(node)
	if err != nil {
		e.logger.Warn("polaris: bootstrap persist failed", "error", err)
	} else {
		e.logger.Info("polaris: bootstrap summary created",
			"id", id, "session", sessionKey,
			"range", [2]int{0, coveredEnd},
			"olderMessages", len(olderChatMsgs),
			"coveredMessages", covered,
			"summaryTokens", node.TokenEst)
	}

	return enriched
}

// ShouldCompact evaluates whether proactive compaction is needed.
func (e *Engine) ShouldCompact(sessionKey string, currentTokens, budgetTokens int) CompactUrgency {
	if budgetTokens <= 0 {
		return CompactNone
	}
	ratio := float64(currentTokens) / float64(budgetTokens)
	if ratio >= e.cfg.HardThresholdPct {
		return CompactHard
	}
	if ratio >= e.cfg.SoftThresholdPct {
		return CompactSoft
	}
	return CompactNone
}

// Compile-time interface compliance.
var _ compact.Summarizer = (*capturingSummarizer)(nil)

// capturingSummarizer wraps a Summarizer to capture the last summary output.
// The mutex guards *captured because compaction.summarizeInChunks fans out
// multiple goroutines that each call Summarize on the shared summarizer; without
// the lock the concurrent writes to the captured pointer race (last-writer-wins
// semantics are preserved — the test confirms only that *something* was captured).
type capturingSummarizer struct {
	mu       sync.Mutex
	inner    compact.Summarizer
	captured *string
}

func (c *capturingSummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	result, err := c.inner.Summarize(ctx, system, conversation, maxOutputTokens)
	if err == nil && result != "" {
		c.mu.Lock()
		*c.captured = result
		c.mu.Unlock()
	}
	return result, err
}
