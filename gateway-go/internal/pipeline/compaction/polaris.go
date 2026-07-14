// Polaris compaction system: tiered context compression for long-running agent sessions.
//
// Three tiers applied in order:
//  1. Emergency — single user input ≥30K tokens: evict oldest messages, compact remaining
//  2. Micro     — strip code fences from tool results older than 4 turns (no LLM call)
//  3. LLM       — at 90% of context budget: local AI summarizes old messages to 20% target
package compaction

import (
	"context"
	"encoding/json"
	"log/slog"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const (
	DefaultMicroTurnThreshold      = 4
	DefaultLLMThresholdPct         = 0.90
	DefaultLLMTargetPct            = 0.20
	DefaultEmergencyInputThreshold = 30_000
	// DefaultStubMinChars is the rune threshold above which an old
	// tool_result block's content is replaced with a short placeholder by
	// TruncateOldToolResults (Tier 2b cheap pruning). 256 runes ≈ 128
	// tokens for CJK content; below this stubbing yields little gain
	// while losing the entire result.
	DefaultStubMinChars = 256
	// CheapPruneMinUsagePct gates the Tier 2/2b cheap pruning: below this
	// fraction of the context budget they are skipped entirely. Pruning is
	// recomputed from the raw transcript every turn with a moving 4-turn
	// cutoff, so each turn it rewrites bytes mid-history — on the local-vLLM
	// path that invalidates the prefix cache (APC) from that point and forces
	// a re-prefill of the recent tail on every call. Far from the budget that
	// trade is a pure loss (the model handles the un-pruned context fine; the
	// measured dsv4 decode rate is flat to 250K input). The gate keys on the
	// pre-pruning estimate, which only shrinks when a Tier 3 summary persists
	// — so it cannot oscillate at the boundary.
	CheapPruneMinUsagePct = 0.50
	runesPerToken         = 2
)

// Config holds Polaris compaction parameters.
type Config struct {
	ContextBudget     int  // effective token budget (MemoryTokenBudget - SystemPromptBudget)
	SkipLLMCompaction bool // skip LLM summarization tier (e.g. when summaries already injected)

	// Embedder is an optional embedding client for MMR-based extractive
	// compaction. Used as a fallback when LLM summarization fails.
	Embedder Embedder

	// AnchorKeywords are wiki Tier1 page titles whose facts MUST be preserved
	// through summarization. Passed as a soft hint to the summarizer's system
	// prompt so the LLM emphasizes related facts as inevictable.
	AnchorKeywords []string

	// LearnedGuidelines are short preservation rules distilled from past
	// compaction misses (ACON-style: refine the compression guideline from
	// failures). Additive soft hints appended to the summarizer prompt — they
	// only ever add "preserve X", never relax the hardcoded rules.
	LearnedGuidelines []string

	// PreviousSummary, when non-empty, switches LLM compaction from
	// summarize-from-scratch to incremental UPDATE: the prior summary is fed
	// alongside the new turns and the model is asked to update it (move items
	// In Progress → Done, prune obsolete) rather than re-summarize. Mirrors
	// Hermes Agent's _previous_summary. The caller is responsible for storing
	// Result.Summary and threading it back in as PreviousSummary next time.
	PreviousSummary string
}

// NewConfig creates a compaction config for the given context budget.
// contextBudget should be (MemoryTokenBudget - SystemPromptBudget).
func NewConfig(contextBudget int) Config {
	return Config{ContextBudget: contextBudget}
}

// Result reports what the pipeline did.
type Result struct {
	MicroPruned           int  // tool_result blocks that had code stripped (Tier 2)
	OldToolResultsStubbed int  // tool_result blocks whose content was replaced with a placeholder (Tier 2b)
	LLMCompacted          bool // whether LLM summarization was applied
	EmbeddingCompacted    bool // whether embedding+MMR selection was applied (tier 2 fallback)
	RecencyCompacted      bool // whether recency window was applied (tier 3 fallback)
	EmergencyEvicted      int  // messages evicted due to large input
	TokensBefore          int
	TokensAfter           int
	// Summary is the LLM summary produced by this compaction (Tier 3a), if any.
	// The caller persists it and feeds it back as Config.PreviousSummary next
	// time so the following compaction updates it instead of re-summarizing.
	Summary string
}

// Summarizer provides LLM-based summarization (typically local AI).
// system is the instruction prompt, conversation is the serialized messages.
// maxOutputTokens caps the LLM response length (not input).
type Summarizer interface {
	Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error)
}

// Compact applies the full Polaris pipeline to assembled context messages.
// summarizer may be nil — LLM and emergency compaction are skipped in that case.
func Compact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, Result) {
	result := Result{TokensBefore: EstimateMessagesTokens(messages)}

	// Snapshot file reads before compaction so we can restore them afterward.
	// This preserves file contents the agent was actively editing.
	fileReads := ExtractRecentFileReads(messages)

	var emergencyFired bool
	messages, emergencyFired, result.EmergencyEvicted = runEmergencyCompaction(
		ctx, cfg, messages, summarizer, logger,
	)
	messages, result.MicroPruned, result.OldToolResultsStubbed = runCheapPruning(
		cfg, messages, result.TokensBefore, emergencyFired, logger,
	)
	if !emergencyFired {
		messages, result = runFallbackCompaction(ctx, cfg, messages, summarizer, logger, result)
	}

	if result.didCompact() {
		messages = restoreFileReads(messages, fileReads, logger)
	}

	// Repair any tool_use↔tool_result pair the tiers split across a cut/selection
	// boundary, so the compacted transcript never carries an orphan that Anthropic
	// rejects with a 400 (and that re-sending would wedge into until /reset).
	if result.didCompact() {
		messages = BalanceToolBlocks(messages)
	}

	result.TokensAfter = EstimateMessagesTokens(messages)
	return messages, result
}

func runEmergencyCompaction(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, bool, int) {
	if lastUserInputTokens(messages) < DefaultEmergencyInputThreshold || summarizer == nil {
		return messages, false, 0
	}
	// Image blocks are irrelevant to the summary and can make an emergency LLM
	// request exceed its prompt budget. File restoration still uses the original
	// messages captured by the caller.
	compacted, evicted := EmergencyCompact(
		ctx, cfg, StripImageBlocks(messages), summarizer, logger,
	)
	if evicted == 0 {
		return messages, false, 0
	}
	return compacted, true, evicted
}

func runCheapPruning(
	cfg Config,
	messages []llm.Message,
	tokensBefore int,
	emergencyFired bool,
	logger *slog.Logger,
) ([]llm.Message, int, int) {
	underPressure := emergencyFired || cfg.ContextBudget <= 0 ||
		tokensBefore > int(float64(cfg.ContextBudget)*CheapPruneMinUsagePct)
	if !underPressure {
		if logger != nil {
			logger.Debug("polaris: cheap pruning skipped (context far from budget)",
				"tokens", tokensBefore, "budget", cfg.ContextBudget)
		}
		return messages, 0, 0
	}

	messages, pruned := MicroCompact(messages, DefaultMicroTurnThreshold)
	messages, stubbed := TruncateOldToolResults(
		messages, DefaultMicroTurnThreshold, DefaultStubMinChars,
	)
	if stubbed > 0 && logger != nil {
		logger.Info("polaris: stubbed old tool results", "count", stubbed)
	}
	return messages, pruned, stubbed
}

func runFallbackCompaction(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
	result Result,
) ([]llm.Message, Result) {
	if cfg.SkipLLMCompaction || EstimateMessagesTokens(messages) <= int(float64(cfg.ContextBudget)*DefaultLLMThresholdPct) {
		return messages, result
	}
	if summarizer != nil {
		if compacted, summary, ok := LLMCompact(ctx, cfg, messages, summarizer, logger); ok {
			result.LLMCompacted = true
			result.Summary = summary
			return compacted, result
		}
	}
	if cfg.Embedder != nil {
		if compacted, ok := EmbeddingCompact(ctx, cfg, messages, cfg.Embedder, logger); ok {
			result.EmbeddingCompacted = true
			return compacted, result
		}
	}
	if compacted, ok := RecencyCompact(cfg, messages, logger); ok {
		result.RecencyCompacted = true
		return compacted, result
	}
	return messages, result
}

func (r Result) didCompact() bool {
	return r.LLMCompacted || r.EmbeddingCompacted || r.RecencyCompacted || r.EmergencyEvicted > 0
}

func restoreFileReads(
	messages []llm.Message,
	fileReads []FileReadRecord,
	logger *slog.Logger,
) []llm.Message {
	if len(fileReads) == 0 {
		return messages
	}
	restored := BuildRestorationMessages(fileReads, restorationBudgetTokens)
	if len(restored) == 0 {
		return messages
	}
	insertIdx := lastUserMessageIndex(messages)
	result := make([]llm.Message, 0, len(messages)+len(restored))
	result = append(result, messages[:insertIdx]...)
	result = append(result, restored...)
	result = append(result, messages[insertIdx:]...)
	if logger != nil {
		logger.Info("polaris: restored file reads after compaction", "files", len(fileReads))
	}
	return result
}

func lastUserMessageIndex(messages []llm.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && !isToolResultMessage(json.RawMessage(messages[index].Content.Bytes())) {
			return index
		}
	}
	return len(messages)
}

// EstimateMessagesTokens estimates total tokens across all messages.
func EstimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		// role overhead (~4 tokens) + content
		total += EstimateTokens(m.Content.String()) + 4
	}
	return total
}

// EstimateTokens estimates token count (Korean-calibrated: ~2 runes/token).
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	est := n / runesPerToken
	if est < 1 {
		return 1
	}
	return est
}

// lastUserInputTokens returns estimated tokens of the last real user input message.
// Skips tool_result messages (role=user but content is tool_result blocks) since
// those are system-generated and should not trigger emergency compaction.
func lastUserInputTokens(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if isToolResultMessage(json.RawMessage(messages[i].Content.Bytes())) {
			continue
		}
		return EstimateTokens(messages[i].Content.String())
	}
	return 0
}

// IsToolResultMessage reports whether a message's content is a tool_result
// block array (a system-generated result) rather than real user text. Exported
// so the Polaris engine can snap background-compaction coverage boundaries off
// tool_use↔tool_result pairs without duplicating the parse.
func IsToolResultMessage(content json.RawMessage) bool { return isToolResultMessage(content) }

// isToolResultMessage checks if a user message contains tool_result blocks.
func isToolResultMessage(content json.RawMessage) bool {
	if len(content) == 0 || content[0] != '[' {
		return false // plain string or empty — real user input
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// snapWindowStart advances a kept-window start index past any leading
// tool_result messages so the kept window never begins with a tool_result whose
// matching tool_use sits in the evicted/summarized region (which would orphan
// it). Mirrors OpenClaw excluding tool_result from compaction cut points: the
// result is pushed into the dropped/summarized side together with its
// already-excluded tool_use, instead of surviving as an orphan that
// BalanceToolBlocks must replace with a lossy stub. BalanceToolBlocks remains
// the backstop for the tiers this does not cover (Embedding/MMR). Returns
// len(messages) when every message from startIdx on is a tool_result — callers
// guard against an empty window.
func snapWindowStart(messages []llm.Message, startIdx int) int {
	for startIdx < len(messages) && isToolResultMessage(json.RawMessage(messages[startIdx].Content.Bytes())) {
		startIdx++
	}
	return startIdx
}
