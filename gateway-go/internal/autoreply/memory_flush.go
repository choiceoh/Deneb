// memory_flush.go — Conversation → persistent memory flush.
// Mirrors src/auto-reply/reply/memory-flush.ts (225 LOC).
//
// Before auto-compaction, this subsystem flushes durable memories from the
// conversation into date-stamped markdown files (memory/YYYY-MM-DD.md).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"fmt"
	"strings"
	"time"
)

// Default memory flush thresholds.
const (
	DefaultMemoryFlushSoftTokens           = 4000
	DefaultMemoryFlushForceTranscriptBytes = 2 * 1024 * 1024 // 2 MB
	DefaultPiCompactionReserveTokensFloor  = 8192
)

// tokens.SilentReplyToken is already declared in tokens.go as the marker
// that suppresses delivery of agent replies. Use that constant.

// Memory flush hint strings embedded in prompts.
const (
	memoryFlushTargetHint   = "Store durable memories only in memory/YYYY-MM-DD.md (create memory/ if needed)."
	memoryFlushAppendHint   = "If memory/YYYY-MM-DD.md already exists, APPEND new content only and do not overwrite existing entries."
	memoryFlushReadOnlyHint = "Treat workspace bootstrap/reference files such as MEMORY.md, SOUL.md, TOOLS.md, and CLAUDE.md as read-only during this flush; never overwrite, replace, or edit them."
)

// DefaultMemoryFlushPrompt is the default user-turn prompt for memory flush.
var DefaultMemoryFlushPrompt = strings.Join([]string{
	"Pre-compaction memory flush.",
	memoryFlushTargetHint,
	memoryFlushReadOnlyHint,
	memoryFlushAppendHint,
	"Do NOT create timestamped variant files (e.g., YYYY-MM-DD-HHMM.md); always use the canonical YYYY-MM-DD.md filename.",
	fmt.Sprintf("If nothing to store, reply with %s.", tokens.SilentReplyToken),
}, " ")

// DefaultMemoryFlushSystemPrompt is the default system prompt for memory flush.
var DefaultMemoryFlushSystemPrompt = strings.Join([]string{
	"Pre-compaction memory flush turn.",
	"The session is near auto-compaction; capture durable memories to disk.",
	memoryFlushTargetHint,
	memoryFlushReadOnlyHint,
	memoryFlushAppendHint,
	fmt.Sprintf("You may reply, but usually %s is correct.", tokens.SilentReplyToken),
}, " ")

// MemoryFlushSettings holds the resolved settings for a memory flush.
type MemoryFlushSettings struct {
	Enabled                   bool   `json:"enabled"`
	SoftThresholdTokens       int    `json:"softThresholdTokens"`
	ForceFlushTranscriptBytes int    `json:"forceFlushTranscriptBytes"`
	Prompt                    string `json:"prompt"`
	SystemPrompt              string `json:"systemPrompt"`
	ReserveTokensFloor        int    `json:"reserveTokensFloor"`
}

// ResolveMemoryFlushSettings resolves the effective memory flush settings
// from configuration, applying defaults and safety hints.
func ResolveMemoryFlushSettings(cfg *MemoryFlushConfig) *MemoryFlushSettings {
	softTokens := DefaultMemoryFlushSoftTokens
	if cfg != nil && cfg.SoftThresholdTokens > 0 {
		softTokens = cfg.SoftThresholdTokens
	}

	forceBytes := DefaultMemoryFlushForceTranscriptBytes
	if cfg != nil && cfg.ForceFlushTranscriptBytes > 0 {
		forceBytes = cfg.ForceFlushTranscriptBytes
	}

	prompt := DefaultMemoryFlushPrompt
	if cfg != nil && cfg.Prompt != "" {
		prompt = strings.TrimSpace(cfg.Prompt)
	}
	prompt = ensureMemoryFlushSafetyHints(prompt)
	prompt = ensureNoReplyHint(prompt)

	systemPrompt := DefaultMemoryFlushSystemPrompt
	if cfg != nil && cfg.SystemPrompt != "" {
		systemPrompt = strings.TrimSpace(cfg.SystemPrompt)
	}
	systemPrompt = ensureMemoryFlushSafetyHints(systemPrompt)
	systemPrompt = ensureNoReplyHint(systemPrompt)

	return &MemoryFlushSettings{
		Enabled:                   true,
		SoftThresholdTokens:       softTokens,
		ForceFlushTranscriptBytes: forceBytes,
		Prompt:                    prompt,
		SystemPrompt:              systemPrompt,
		ReserveTokensFloor:        DefaultPiCompactionReserveTokensFloor,
	}
}

// MemoryFlushConfig holds user-configurable memory flush overrides.
type MemoryFlushConfig struct {
	SoftThresholdTokens       int    `json:"softThresholdTokens,omitempty"`
	ForceFlushTranscriptBytes int    `json:"forceFlushTranscriptBytes,omitempty"`
	Prompt                    string `json:"prompt,omitempty"`
	SystemPrompt              string `json:"systemPrompt,omitempty"`
}

// ResolveMemoryFlushRelativePath returns the date-stamped file path for memory storage.
// Uses the given timezone to compute the date stamp.
func ResolveMemoryFlushRelativePath(nowMs int64, timezone string) string {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	t := time.UnixMilli(nowMs)

	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err == nil {
			t = t.In(loc)
		}
	}

	dateStamp := t.Format("2006-01-02")
	return fmt.Sprintf("memory/%s.md", dateStamp)
}

// ResolveMemoryFlushPromptForRun injects date stamps and current-time lines
// into the memory flush prompt.
func ResolveMemoryFlushPromptForRun(prompt string, nowMs int64, timezone string) string {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	t := time.UnixMilli(nowMs)
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err == nil {
			t = t.In(loc)
		}
	}

	dateStamp := t.Format("2006-01-02")
	withDate := strings.ReplaceAll(strings.TrimRight(prompt, " \t\n"), "YYYY-MM-DD", dateStamp)
	if withDate == "" {
		return fmt.Sprintf("Current time: %s", t.Format(time.RFC3339))
	}
	if strings.Contains(withDate, "Current time:") {
		return withDate
	}
	return fmt.Sprintf("%s\nCurrent time: %s", withDate, t.Format(time.RFC3339))
}

// ShouldRunMemoryFlush determines whether a pre-compaction memory flush should run.
func ShouldRunMemoryFlush(params ShouldRunMemoryFlushParams) bool {
	totalTokens := params.TotalTokens
	if totalTokens <= 0 {
		return false
	}

	contextWindow := params.ContextWindowTokens
	if contextWindow <= 0 {
		contextWindow = 1
	}
	reserveTokens := params.ReserveTokensFloor
	if reserveTokens < 0 {
		reserveTokens = 0
	}
	softThreshold := params.SoftThresholdTokens
	if softThreshold < 0 {
		softThreshold = 0
	}

	threshold := contextWindow - reserveTokens - softThreshold
	if threshold <= 0 {
		return false
	}
	if totalTokens < threshold {
		return false
	}

	// Check if already flushed for the current compaction cycle.
	if params.HasMemoryFlushCount && HasAlreadyFlushedForCurrentCompaction(params.CompactionCount, params.MemoryFlushCompactionCount) {
		return false
	}

	return true
}

// ShouldRunMemoryFlushParams holds the inputs for the memory flush gating check.
type ShouldRunMemoryFlushParams struct {
	TotalTokens                int
	ContextWindowTokens        int
	ReserveTokensFloor         int
	SoftThresholdTokens        int
	CompactionCount            int
	MemoryFlushCompactionCount int  // -1 means "never flushed"
	HasMemoryFlushCount        bool // true if MemoryFlushCompactionCount was explicitly set
	// Transcript byte-size trigger (0 = disabled).
	TranscriptBytes           int
	ForceFlushTranscriptBytes int
}

// ShouldForceMemoryFlushByTranscriptSize returns true when the session transcript
// exceeds the byte-size threshold, regardless of token counts. This catches cases
// where the token counter is stale but the transcript is known to be large.
func ShouldForceMemoryFlushByTranscriptSize(params ShouldRunMemoryFlushParams) bool {
	if params.ForceFlushTranscriptBytes <= 0 || params.TranscriptBytes <= 0 {
		return false
	}
	if params.TranscriptBytes < params.ForceFlushTranscriptBytes {
		return false
	}
	if params.HasMemoryFlushCount && HasAlreadyFlushedForCurrentCompaction(params.CompactionCount, params.MemoryFlushCompactionCount) {
		return false
	}
	return true
}

// HasAlreadyFlushedForCurrentCompaction returns true when a memory flush has
// already been performed for the current compaction cycle.
func HasAlreadyFlushedForCurrentCompaction(compactionCount, memoryFlushCompactionCount int) bool {
	return memoryFlushCompactionCount == compactionCount
}

// ensureNoReplyHint appends the NO_REPLY hint if not already present.
func ensureNoReplyHint(text string) string {
	if strings.Contains(text, tokens.SilentReplyToken) {
		return text
	}
	return fmt.Sprintf("%s\n\nIf no user-visible reply is needed, start with %s.", text, tokens.SilentReplyToken)
}

// ensureMemoryFlushSafetyHints ensures all required safety hints are present.
func ensureMemoryFlushSafetyHints(text string) string {
	next := strings.TrimSpace(text)
	requiredHints := []string{
		memoryFlushTargetHint,
		memoryFlushAppendHint,
		memoryFlushReadOnlyHint,
	}
	for _, hint := range requiredHints {
		if !strings.Contains(next, hint) {
			if next == "" {
				next = hint
			} else {
				next = next + "\n\n" + hint
			}
		}
	}
	return next
}
