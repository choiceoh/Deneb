package polaris

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

// summaryPrefix is injected by AssembleContext into summary messages.
// Used to detect already-summarized content and skip re-summarization.
const summaryPrefix = "[이전 대화 요약"

const (
	// bootstrapRawThreshold: if older messages are below this, inject raw (no LLM).
	bootstrapRawThreshold = 50_000
)

// Engine orchestrates Polaris compaction with DAG persistence and condensation.
// Long-lived: stored on Bridge, shared across runs for the same session.
type Engine struct {
	store    *Store
	logger   *slog.Logger
	cfg      Config
	circuit  *CircuitBreaker
	embedder compact.Embedder // optional; BGE-M3 for MMR compaction fallback
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

// CompactAndPersist runs Polaris compaction and persists any LLM summary
// into the DAG as a leaf node. Skips re-summarization when the context
// already contains summary messages from AssembleContext.
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
	// Bootstrap: recover older messages dropped by freshTailCount.
	messages = e.bootstrapIfNeeded(ctx, sessionKey, messages, summarizer)

	polarisCfg := compact.NewConfig(contextBudget)
	polarisCfg.Embedder = e.embedder

	// Summary reuse: if context already has injected summaries (from
	// AssembleContext or bootstrap), Polaris would re-summarize them. Detect and skip.
	if hasSummaryMessages(messages) {
		// Polaris can still micro-compact and handle emergency, but the
		// LLM tier should not re-summarize our injected summaries.
		polarisCfg.SkipLLMCompaction = true
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

	// Run Polaris 3-tier compaction.
	compacted, result := compact.Compact(ctx, polarisCfg, messages, wrappedSummarizer, e.logger)

	// Persist summary to DAG if any summarization occurred.
	if capturedSummary != "" && (result.LLMCompacted || result.EmergencyEvicted > 0) {
		e.persistSummary(sessionKey, capturedSummary, len(compacted))
	}

	// Safety-net: if compaction could not bring the context within budget
	// (e.g. recent messages themselves are extremely large and LLM compaction
	// preserved them), drop oldest messages as a last resort. This should be
	// rare; the warn log helps diagnose when it fires.
	if contextBudget > 0 && compact.EstimateMessagesTokens(compacted) > contextBudget {
		before := compact.EstimateMessagesTokens(compacted)
		compacted = trimLLMToTokenBudget(compacted, contextBudget)
		after := compact.EstimateMessagesTokens(compacted)
		e.logger.Warn("polaris: post-compaction safety trim fired",
			"tokensBefore", before, "tokensAfter", after, "budget", contextBudget)
	}

	return compacted, result
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

	summary := compact.BootstrapCompact(ctx, olderLLM, summarizer, e.logger)
	if summary == "" {
		return messages
	}

	// Inject summary message at the front of context.
	summaryText := fmt.Sprintf("%s (메시지 0-%d)]\n\n%s", summaryPrefix, olderEnd, summary)
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
		MsgEnd:     olderEnd,
	}
	id, err := e.store.InsertSummary(node)
	if err != nil {
		e.logger.Warn("polaris: bootstrap persist failed", "error", err)
	} else {
		e.logger.Info("polaris: bootstrap summary created",
			"id", id, "session", sessionKey,
			"range", [2]int{0, olderEnd},
			"olderMessages", len(olderChatMsgs),
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

// hasSummaryMessages checks if any message starts with the summary prefix,
// indicating AssembleContext already injected summaries.
func hasSummaryMessages(messages []llm.Message) bool {
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(m.Content, &text) == nil && strings.HasPrefix(text, summaryPrefix) {
			return true
		}
	}
	return false
}

// Compile-time interface compliance.
var _ compact.Summarizer = (*capturingSummarizer)(nil)

// capturingSummarizer wraps a Summarizer to capture the last summary output.
type capturingSummarizer struct {
	inner    compact.Summarizer
	captured *string
}

func (c *capturingSummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	result, err := c.inner.Summarize(ctx, system, conversation, maxOutputTokens)
	if err == nil && result != "" {
		*c.captured = result
	}
	return result, err
}
