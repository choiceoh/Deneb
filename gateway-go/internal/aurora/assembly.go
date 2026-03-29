// Assembly orchestrator: host-side driver for the Rust context assembly engine.
//
// Selects messages and summaries from the Aurora store that fit within the
// token budget, using the DAG-aware fresh-tail + eviction algorithm in core-rs.
package aurora

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// AssemblyConfig configures context assembly behavior.
type AssemblyConfig struct {
	TokenBudget    uint64 `json:"tokenBudget"`
	FreshTailCount uint32 `json:"freshTailCount"`
	MaxMessages    int    `json:"maxMessages"` // fallback limit when FFI unavailable
}

// DefaultAssemblyConfig returns production defaults.
func DefaultAssemblyConfig() AssemblyConfig {
	return AssemblyConfig{
		TokenBudget:    100_000,
		FreshTailCount: 32,
		MaxMessages:    100,
	}
}

// AssemblyResult holds the output of context assembly.
type AssemblyResult struct {
	Messages             []llm.Message
	SystemPromptAddition string // Aurora guidance text to append to system prompt
	EstimatedTokens      int
	TotalMessages        int
	SummaryCount         int
}

// Assemble selects messages and summaries from the Aurora store that fit
// within the token budget. Uses Rust FFI when available; falls back to
// simple tail-N selection.
func Assemble(
	store *Store,
	conversationID uint64,
	cfg AssemblyConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	if ffi.Available {
		return assembleFFI(store, conversationID, cfg, logger)
	}
	return assembleFallback(store, conversationID, cfg, logger)
}

func assembleFFI(
	store *Store,
	conversationID uint64,
	cfg AssemblyConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	handle, err := ffi.ContextAssemblyNew(conversationID, cfg.TokenBudget, cfg.FreshTailCount)
	if err != nil {
		logger.Warn("aurora assembly: FFI failed, using fallback", "error", err)
		return assembleFallback(store, conversationID, cfg, logger)
	}
	defer ffi.ContextEngineDrop(handle)

	cmdJSON, err := ffi.ContextAssemblyStart(handle)
	if err != nil {
		return nil, fmt.Errorf("aurora assembly: start: %w", err)
	}

	const maxIterations = 50
	for i := 0; i < maxIterations; i++ {
		var cmd struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
			return nil, fmt.Errorf("aurora assembly: parse cmd: %w", err)
		}

		if cmd.Type == "done" {
			return parseAssemblyDone(store, cmdJSON)
		}

		resp, err := handleAssemblyCmd(store, cmdJSON)
		if err != nil {
			return nil, fmt.Errorf("aurora assembly: handle %s: %w", cmd.Type, err)
		}

		respJSON, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("aurora assembly: marshal resp: %w", err)
		}

		cmdJSON, err = ffi.ContextAssemblyStep(handle, respJSON)
		if err != nil {
			return nil, fmt.Errorf("aurora assembly: step: %w", err)
		}
	}

	return nil, fmt.Errorf("aurora assembly: exceeded %d iterations", maxIterations)
}

func handleAssemblyCmd(store *Store, cmdJSON json.RawMessage) (any, error) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}
	convID := parseConversationID(cmdJSON)

	switch cmd.Type {
	case "fetchContextItems":
		items, err := store.FetchContextItems(convID)
		if err != nil {
			return nil, err
		}
		// Build assembly-compatible items with token counts.
		asmItems := make([]map[string]any, 0, len(items))
		for _, ci := range items {
			item := map[string]any{
				"ordinal":     ci.Ordinal,
				"itemType":    ci.ItemType,
				"depth":       uint32(0),
				"isCondensed": false,
			}

			if ci.ItemType == "message" && ci.MessageID != nil {
				msgs, err := store.FetchMessages([]uint64{*ci.MessageID})
				if err != nil {
					return nil, err
				}
				if m, ok := msgs[*ci.MessageID]; ok {
					item["messageId"] = m.MessageID
					item["tokenCount"] = m.TokenCount
				} else {
					item["messageId"] = ci.MessageID
					item["tokenCount"] = uint64(0)
				}
			} else if ci.ItemType == "summary" && ci.SummaryID != nil {
				sums, err := store.FetchSummaries([]string{*ci.SummaryID})
				if err != nil {
					return nil, err
				}
				if s, ok := sums[*ci.SummaryID]; ok {
					item["summaryId"] = s.SummaryID
					item["tokenCount"] = s.TokenCount
					item["depth"] = s.Depth
					item["isCondensed"] = s.Kind == "condensed"
				} else {
					item["summaryId"] = ci.SummaryID
					item["tokenCount"] = uint64(0)
				}
			}
			asmItems = append(asmItems, item)
		}
		return map[string]any{
			"type":  "contextItems",
			"items": asmItems,
		}, nil

	case "fetchSummaryStats":
		stats, err := store.FetchSummaryStats(convID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":  "summaryStats",
			"stats": stats,
		}, nil

	default:
		return map[string]any{"type": "empty"}, nil
	}
}

func parseAssemblyDone(store *Store, cmdJSON json.RawMessage) (*AssemblyResult, error) {
	var envelope struct {
		Result struct {
			EstimatedTokens      int      `json:"estimatedTokens"`
			RawMessageCount      int      `json:"rawMessageCount"`
			SummaryCount         int      `json:"summaryCount"`
			SystemPromptAddition string   `json:"systemPromptAddition"`
			SelectedItemIDs      []string `json:"selectedItemIds,omitempty"`
		} `json:"result"`
	}
	if err := json.Unmarshal(cmdJSON, &envelope); err != nil {
		return nil, fmt.Errorf("parse assembly done: %w", err)
	}
	done := envelope.Result

	// Parse selectedItemIds from the Rust engine.
	// Format: "msg_{messageId}" for messages, raw summary ID for summaries.
	type selectedItem struct {
		isMessage bool
		msgID     uint64
		sumID     string
	}
	var items []selectedItem
	var msgIDs []uint64
	var sumIDs []string

	for _, id := range done.SelectedItemIDs {
		if strings.HasPrefix(id, "msg_") {
			if n, err := strconv.ParseUint(id[4:], 10, 64); err == nil {
				items = append(items, selectedItem{isMessage: true, msgID: n})
				msgIDs = append(msgIDs, n)
			}
		} else {
			items = append(items, selectedItem{isMessage: false, sumID: id})
			sumIDs = append(sumIDs, id)
		}
	}

	// Fetch full records.
	messages, err := store.FetchMessages(msgIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch selected messages: %w", err)
	}
	summaries, err := store.FetchSummaries(sumIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch selected summaries: %w", err)
	}

	// Build ordered LLM messages preserving the assembly's selected order.
	// Insert a boundary marker at the transition from summaries to fresh messages
	// so the LLM can distinguish summarized history from recent conversation.
	var llmMsgs []llm.Message
	hasSummaries := false
	boundaryInserted := false

	for _, item := range items {
		if item.isMessage {
			if hasSummaries && !boundaryInserted {
				llmMsgs = append(llmMsgs, llm.NewTextMessage("user",
					"─── Context boundary: above is summarized history, below is recent conversation ───"))
				boundaryInserted = true
			}
			if m, ok := messages[item.msgID]; ok {
				role := m.Role
				if role == "" {
					role = "user"
				}
				llmMsgs = append(llmMsgs, llm.NewTextMessage(role, m.Content))
			}
		} else {
			hasSummaries = true
			if s, ok := summaries[item.sumID]; ok {
				prefix := "[Aurora Summary"
				if s.Kind == "condensed" {
					prefix += fmt.Sprintf(" depth=%d", s.Depth)
				}
				prefix += "]"
				llmMsgs = append(llmMsgs, llm.NewTextMessage(
					"user",
					fmt.Sprintf("%s\n%s", prefix, s.Content),
				))
			}
		}
	}

	return &AssemblyResult{
		Messages:             llmMsgs,
		SystemPromptAddition: done.SystemPromptAddition,
		EstimatedTokens:      done.EstimatedTokens,
		TotalMessages:        done.RawMessageCount,
		SummaryCount:         done.SummaryCount,
	}, nil
}

// assembleFallback loads the most recent messages when FFI is unavailable.
func assembleFallback(
	store *Store,
	conversationID uint64,
	cfg AssemblyConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	items, err := store.FetchContextItems(conversationID)
	if err != nil {
		return nil, err
	}

	// Take the last N items.
	limit := cfg.MaxMessages
	if limit <= 0 {
		limit = 100
	}
	if len(items) > limit {
		items = items[len(items)-limit:]
	}

	// Collect message/summary IDs.
	var msgIDs []uint64
	var sumIDs []string
	for _, ci := range items {
		if ci.ItemType == "message" && ci.MessageID != nil {
			msgIDs = append(msgIDs, *ci.MessageID)
		} else if ci.ItemType == "summary" && ci.SummaryID != nil {
			sumIDs = append(sumIDs, *ci.SummaryID)
		}
	}

	messages, err := store.FetchMessages(msgIDs)
	if err != nil {
		logger.Error("assembleFallback: FetchMessages failed, context may be incomplete", "error", err)
		return nil, fmt.Errorf("assembleFallback: fetch messages: %w", err)
	}
	summaries, err := store.FetchSummaries(sumIDs)
	if err != nil {
		logger.Error("assembleFallback: FetchSummaries failed, context may be incomplete", "error", err)
		return nil, fmt.Errorf("assembleFallback: fetch summaries: %w", err)
	}

	var llmMsgs []llm.Message
	hasSums := false
	boundaryDone := false

	for _, ci := range items {
		if ci.ItemType == "summary" && ci.SummaryID != nil {
			hasSums = true
			if s, ok := summaries[*ci.SummaryID]; ok {
				llmMsgs = append(llmMsgs, llm.NewTextMessage("user",
					fmt.Sprintf("[Aurora Summary]\n%s", s.Content)))
			}
		} else if ci.ItemType == "message" && ci.MessageID != nil {
			if hasSums && !boundaryDone {
				llmMsgs = append(llmMsgs, llm.NewTextMessage("user",
					"─── Context boundary: above is summarized history, below is recent conversation ───"))
				boundaryDone = true
			}
			if m, ok := messages[*ci.MessageID]; ok {
				role := m.Role
				if role == "" {
					role = "user"
				}
				llmMsgs = append(llmMsgs, llm.NewTextMessage(role, m.Content))
			}
		}
	}

	return &AssemblyResult{
		Messages:      llmMsgs,
		TotalMessages: len(items),
		SummaryCount:  len(sumIDs),
	}, nil
}
