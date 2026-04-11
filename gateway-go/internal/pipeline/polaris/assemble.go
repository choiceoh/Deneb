package polaris

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

// assembleContextFull builds the LLM context from the Polaris store.
//
// Algorithm:
//  1. Query latest summary coverage and total message count.
//  2. Recent window: messages not covered by any summary → raw messages.
//  3. Summary window: highest-level summaries covering older messages.
//  4. Pack into token budget: protected tail (freshTailCount) + summaries newest-first.
//  5. If still over budget, drop oldest summaries (deterministic truncation).
func assembleContextFull(
	store *Store,
	sessionKey string,
	memoryTokenBudget int,
	freshTailCount int,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	// Check if store has data for this session.
	maxIdx, err := store.MaxMsgIndex(sessionKey)
	if err != nil || maxIdx < 0 {
		return &AssemblyResult{}, nil
	}

	// Query summary coverage.
	summaryCoverage, _ := store.LatestSummaryCoverage(sessionKey)

	// Recent window: messages after the latest summary coverage.
	recentStart := summaryCoverage + 1
	recentMsgs, err := store.LoadMessages(sessionKey, recentStart, -1)
	if err != nil {
		return nil, fmt.Errorf("polaris assemble: load recent: %w", err)
	}

	// Convert recent ChatMessages to llm.Messages.
	recent := chatToLLM(recentMsgs)

	// Enforce fresh tail count: keep only the most recent N messages.
	if freshTailCount > 0 && len(recent) > freshTailCount {
		recent = recent[len(recent)-freshTailCount:]
	}

	// If no summaries exist, return recent messages as-is.
	// Compaction will handle overflow (summarize first, truncate only as last resort).
	if summaryCoverage < 0 {
		tokens := compact.EstimateMessagesTokens(recent)
		return &AssemblyResult{
			Messages:        recent,
			EstimatedTokens: tokens,
			TotalMessages:   maxIdx + 1,
		}, nil
	}

	// Load summaries for the covered range.
	// Prefer highest-level summaries (condensed > leaf) for efficiency.
	summaries, err := store.LoadSummaries(sessionKey, 0) // all levels
	if err != nil {
		return nil, fmt.Errorf("polaris assemble: load summaries: %w", err)
	}

	// Select non-overlapping summaries: prefer higher-level nodes.
	// Walk from highest level down, marking covered ranges.
	selected := selectBestSummaries(summaries, summaryCoverage)

	// Build summary messages (newest-first ordering for LLM context).
	var summaryMsgs []llm.Message
	for _, node := range selected {
		text := fmt.Sprintf("[이전 대화 요약 (메시지 %d-%d)]\n\n%s", node.MsgStart, node.MsgEnd, node.Content)
		summaryMsgs = append(summaryMsgs, llm.NewTextMessage("user", text))
	}

	// Calculate token budgets.
	recentTokens := compact.EstimateMessagesTokens(recent)
	summaryTokens := compact.EstimateMessagesTokens(summaryMsgs)
	totalTokens := recentTokens + summaryTokens

	// If over budget, drop oldest summaries first.
	if memoryTokenBudget > 0 && totalTokens > memoryTokenBudget {
		// Recent messages are protected; trim summaries from the oldest.
		remaining := memoryTokenBudget - recentTokens
		if remaining < 0 {
			remaining = 0
		}
		summaryMsgs = trimLLMToTokenBudget(summaryMsgs, remaining)
		summaryTokens = compact.EstimateMessagesTokens(summaryMsgs)
		totalTokens = recentTokens + summaryTokens
	}

	// Assemble: summaries first (oldest context), then recent messages.
	messages := make([]llm.Message, 0, len(summaryMsgs)+len(recent))
	messages = append(messages, summaryMsgs...)
	messages = append(messages, recent...)

	if len(summaryMsgs) > 0 {
		logger.Info("polaris: assembled context with summaries",
			"summaryNodes", len(summaryMsgs),
			"recentMsgs", len(recent),
			"totalTokens", totalTokens)
	}

	return &AssemblyResult{
		Messages:        messages,
		EstimatedTokens: totalTokens,
		TotalMessages:   maxIdx + 1,
		WasCompacted:    len(summaryMsgs) > 0,
	}, nil
}

// selectBestSummaries picks non-overlapping summaries preferring higher levels.
// Returns summaries ordered by MsgStart ascending.
func selectBestSummaries(all []SummaryNode, maxCoverage int) []SummaryNode {
	if len(all) == 0 {
		return nil
	}

	// Find the maximum level.
	maxLevel := 0
	for _, n := range all {
		if n.Level > maxLevel {
			maxLevel = n.Level
		}
	}

	// Greedily pick from highest level down, tracking covered ranges.
	covered := make(map[int]bool) // msg_index → covered
	var selected []SummaryNode

	for level := maxLevel; level >= 1; level-- {
		for _, n := range all {
			if n.Level != level || n.MsgEnd > maxCoverage {
				continue
			}
			// Check if this range is already covered.
			alreadyCovered := false
			for idx := n.MsgStart; idx <= n.MsgEnd; idx++ {
				if covered[idx] {
					alreadyCovered = true
					break
				}
			}
			if alreadyCovered {
				continue
			}
			// Mark range as covered.
			for idx := n.MsgStart; idx <= n.MsgEnd; idx++ {
				covered[idx] = true
			}
			selected = append(selected, n)
		}
	}

	// Sort by MsgStart ascending for chronological order.
	sortByMsgStart(selected)
	return selected
}

// sortByMsgStart sorts summary nodes by MsgStart ascending (insertion sort, small N).
func sortByMsgStart(nodes []SummaryNode) {
	for i := 1; i < len(nodes); i++ {
		key := nodes[i]
		j := i - 1
		for j >= 0 && nodes[j].MsgStart > key.MsgStart {
			nodes[j+1] = nodes[j]
			j--
		}
		nodes[j+1] = key
	}
}

// chatToLLM converts ChatMessage transcript entries to LLM messages.
func chatToLLM(msgs []toolctx.ChatMessage) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}
		out = append(out, llm.Message{Role: role, Content: m.Content})
	}
	return out
}

// trimLLMToTokenBudget drops oldest messages until total fits within budget.
func trimLLMToTokenBudget(msgs []llm.Message, budget int) []llm.Message {
	if budget <= 0 || len(msgs) == 0 {
		return msgs
	}
	total := compact.EstimateMessagesTokens(msgs)
	for len(msgs) > 1 && total > budget {
		total -= compact.EstimateTokens(string(msgs[0].Content)) + 4
		msgs = msgs[1:]
	}
	return msgs
}
