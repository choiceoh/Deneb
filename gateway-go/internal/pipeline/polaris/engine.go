package polaris

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/compaction"
)

// summaryPrefix is injected by AssembleContext into summary messages.
// Used to detect already-summarized content and skip re-summarization.
const summaryPrefix = "[이전 대화 요약"

// Engine orchestrates Polaris compaction with DAG persistence and condensation.
// Long-lived: stored on Bridge, shared across runs for the same session.
type Engine struct {
	store   *Store
	logger  *slog.Logger
	cfg     Config
	circuit *CircuitBreaker
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

// CompactAndPersist runs Polaris compaction and persists any LLM summary
// into the DAG as a leaf node. Skips re-summarization when the context
// already contains summary messages from AssembleContext.
func (e *Engine) CompactAndPersist(
	ctx context.Context,
	sessionKey string,
	messages []llm.Message,
	summarizer compact.Summarizer,
) ([]llm.Message, compact.Result) {
	polarisCfg := compact.DefaultConfig()

	// Summary reuse: if context already has injected summaries (from
	// AssembleContext), Polaris would re-summarize them. Detect and skip.
	if hasSummaryMessages(messages) {
		// Polaris can still micro-compact and handle emergency, but the
		// LLM tier should not re-summarize our injected summaries.
		// Raise the LLM threshold to effectively disable it for this run.
		polarisCfg.LLMThresholdPct = 1.0 // 100% = never trigger
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
