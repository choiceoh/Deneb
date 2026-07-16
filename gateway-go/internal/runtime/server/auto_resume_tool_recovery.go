package server

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

const interruptedToolResult = "Tool execution was interrupted before its result was durably recorded. Retry the call if it is still needed."

type recoveredToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type toolResultRecoveryStats struct {
	recovered int
	missing   int
}

// recoverInterruptedToolTurn reconstructs the ordered user/tool_result message
// for the last assistant tool_use turn. Completed calls reuse their durable
// receipts; calls with no receipt get an explicit interrupted result so the
// model can retry only the missing work.
func recoverInterruptedToolTurn(
	store chatport.TranscriptStore,
	sessionKey string,
	observation transcriptTailObservation,
	notBeforeMs int64,
	nowMs int64,
) (toolResultRecoveryStats, error) {
	var stats toolResultRecoveryStats
	if len(observation.toolUses) == 0 {
		return stats, errors.New("tool_use tail has no identifiable calls")
	}
	receiptStore := chatport.ResolveToolResultReceiptStore(store)
	if receiptStore == nil {
		return stats, errors.New("transcript store has no tool result receipt capability")
	}
	receipts, err := receiptStore.LoadToolResultReceipts(sessionKey)
	if err != nil {
		return stats, err
	}
	latest := make(map[string]chatport.ToolResultReceipt, len(receipts))
	for _, receipt := range receipts {
		if receipt.CompletedAt < notBeforeMs {
			continue
		}
		if current, ok := latest[receipt.ToolUseID]; ok && current.CompletedAt > receipt.CompletedAt {
			continue
		}
		latest[receipt.ToolUseID] = receipt
	}

	blocks := make([]recoveredToolResultBlock, 0, len(observation.toolUses))
	for _, toolUse := range observation.toolUses {
		block := recoveredToolResultBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.id,
			Content:   interruptedToolResult,
			IsError:   true,
		}
		if receipt, ok := latest[toolUse.id]; ok && (receipt.ToolName == "" || receipt.ToolName == toolUse.name) {
			block.Content = receipt.Content
			block.IsError = receipt.IsError
			stats.recovered++
		} else {
			stats.missing++
		}
		blocks = append(blocks, block)
	}

	content, err := json.Marshal(blocks)
	if err != nil {
		return stats, fmt.Errorf("marshal recovered tool results: %w", err)
	}
	if err := store.Append(sessionKey, chatport.ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: nowMs,
	}); err != nil {
		return stats, fmt.Errorf("append recovered tool results: %w", err)
	}
	// The canonical ordered batch is now durable. Cleanup is best-effort: a
	// leftover receipt file is harmless because future recovery matches by the
	// provider-generated tool_use ID.
	_ = receiptStore.DeleteToolResultReceipts(sessionKey)
	return stats, nil
}
