// executor_run_state.go — run-scoped result accumulation and final snapshots.
package agent

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// agentTurnStats contains the derived shape of one completed LLM turn that
// RunAgent needs for logging. Result mutation stays in agentRunState so every
// turn updates usage, text, thinking, and tool aggregates through one path.
type agentTurnStats struct {
	textChars      int
	textHead       string
	toolCount      int
	toolNames      []string
	toolInputBytes int
	thinkingText   string
}

// agentRunState owns the mutable, run-scoped output assembled by RunAgent.
// RunAgent remains responsible for orchestration (stream, recovery/gate, tool
// dispatch); this type keeps result accumulation and terminal snapshots
// consistent across every return path.
type agentRunState struct {
	result  *AgentResult
	journal *runMessageJournal

	allText         strings.Builder
	deliverableText strings.Builder
	thinking        strings.Builder

	totalTextChars       int
	totalToolCalls       int
	toolCounts           map[string]int
	priorToolOutputRunes int
	maxTokenRecoveries   int
}

func newAgentRunState(messages []llm.Message, onPersist func(llm.Message)) *agentRunState {
	return &agentRunState{
		result:     &AgentResult{},
		journal:    newRunMessageJournal(messages, onPersist),
		toolCounts: make(map[string]int),
	}
}

// recordTurn commits one completed provider turn to the run-level result and
// returns the compact derived fields used by RunAgent's diagnostic logging.
func (s *agentRunState) recordTurn(turnRes *turnResult) agentTurnStats {
	s.result.Usage.InputTokens += turnRes.usage.InputTokens
	s.result.Usage.OutputTokens += turnRes.usage.OutputTokens
	s.result.Usage.CacheReadInputTokens += turnRes.usage.CacheReadInputTokens
	s.result.Usage.CacheCreationInputTokens += turnRes.usage.CacheCreationInputTokens

	stats := agentTurnStats{
		textChars:    len(turnRes.text),
		toolCount:    len(turnRes.toolCalls),
		toolNames:    make([]string, 0, len(turnRes.toolCalls)),
		thinkingText: joinAllThinkingTexts(turnRes.contentBlocks),
	}
	if stats.textChars > 200 {
		stats.textHead = textutil.TruncateBytes(turnRes.text, 200) + "…"
	} else {
		stats.textHead = turnRes.text
	}
	for _, tc := range turnRes.toolCalls {
		if tc.Name != "" {
			stats.toolNames = append(stats.toolNames, tc.Name)
			s.toolCounts[tc.Name]++
		}
		stats.toolInputBytes += tc.Input.Len()
	}
	s.totalTextChars += stats.textChars
	s.totalToolCalls += stats.toolCount

	if turnRes.text != "" {
		s.result.Text = turnRes.text
		appendRunSection(&s.allText, turnRes.text)
		if !isInterimNarration(turnRes.text, len(turnRes.toolCalls)) {
			appendRunSection(&s.deliverableText, stripNarrationHead(turnRes.text))
		}
	}
	if stats.thinkingText != "" {
		appendRunSection(&s.thinking, stats.thinkingText)
	}

	return stats
}

func appendRunSection(builder *strings.Builder, text string) {
	if builder.Len() > 0 {
		builder.WriteString("\n\n")
	}
	builder.WriteString(text)
}

// recordToolActivities preserves execution order and maintains the observation
// size seen by the next turn without re-scanning the complete activity history.
func (s *agentRunState) recordToolActivities(activities []ToolActivity) {
	s.result.ToolActivities = append(s.result.ToolActivities, activities...)
	for _, activity := range activities {
		s.priorToolOutputRunes += activity.OutputRunes
	}
}

// noteRecovery records one max-output-token recovery and returns its 1-based
// attempt number for scaling and logging.
func (s *agentRunState) noteRecovery() int {
	s.maxTokenRecoveries++
	return s.maxTokenRecoveries
}

// finalize snapshots every run-scoped aggregate. It is deliberately deferred
// by RunAgent so every exit that returns an AgentResult cannot forget a field
// when adding a new cancellation, timeout, graceful, or budget return path.
func (s *agentRunState) finalize() {
	s.result.AllText = s.allText.String()
	s.result.DeliverableText = s.deliverableText.String()
	s.result.Thinking = s.thinking.String()
	s.result.TurnsPersisted = s.journal.persisted
	s.result.MaxTokensRecoveries = s.maxTokenRecoveries
	s.result.FinalMessages = s.journal.messages
	s.result.TotalTextChars = s.totalTextChars
	s.result.TotalToolCalls = s.totalToolCalls
	s.result.BudgetGraceCall = false

	if len(s.toolCounts) == 0 {
		return
	}
	copied := make(map[string]int, len(s.toolCounts))
	for name, count := range s.toolCounts {
		copied[name] = count
	}
	s.result.ToolCounts = copied
}
