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
