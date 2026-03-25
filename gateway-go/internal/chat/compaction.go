package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Compaction defaults.
const (
	defaultContextThreshold = 0.75
)

// CompactionConfig configures compaction behavior.
type CompactionConfig struct {
	ContextThreshold float64 `json:"contextThreshold"` // fraction of budget (default 0.75)
	FreshTailCount   int     `json:"freshTailCount"`   // messages to protect (default 32)
}

// DefaultCompactionConfig returns sensible defaults.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		ContextThreshold: defaultContextThreshold,
		FreshTailCount:   defaultFreshTailCount,
	}
}

// CompactionDecision is the parsed result from compaction evaluation.
type CompactionDecision struct {
	ShouldCompact bool   `json:"shouldCompact"`
	Reason        string `json:"reason"`
	CurrentTokens uint64 `json:"currentTokens"`
	Threshold     uint64 `json:"threshold"`
}

// evaluateCompaction checks whether context compaction is needed.
func evaluateCompaction(cfg CompactionConfig, storedTokens, liveTokens, tokenBudget uint64) (*CompactionDecision, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal compaction config: %w", err)
	}

	resultJSON, err := ffi.CompactionEvaluate(string(configJSON), storedTokens, liveTokens, tokenBudget)
	if err != nil {
		return nil, fmt.Errorf("compaction evaluate: %w", err)
	}

	var decision CompactionDecision
	if err := json.Unmarshal(resultJSON, &decision); err != nil {
		return nil, fmt.Errorf("parse compaction decision: %w", err)
	}
	return &decision, nil
}

// runCompactionSweep executes a compaction sweep via FFI state machine.
// The sweep condenses old messages into summaries to reduce token usage.
// Returns true if compaction was performed successfully.
func runCompactionSweep(
	store TranscriptStore,
	sessionKey string,
	cfg CompactionConfig,
	tokenBudget uint64,
	logger *slog.Logger,
) (bool, error) {
	if !ffi.Available {
		logger.Info("compaction sweep skipped: FFI unavailable")
		return false, nil
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return false, fmt.Errorf("marshal compaction config: %w", err)
	}

	// Use fixed conversation ID for single-user deployment.
	var conversationID uint64 = 1
	nowMs := time.Now().UnixMilli()

	handle, err := ffi.CompactionSweepNew(
		string(configJSON),
		conversationID,
		tokenBudget,
		false, // force
		true,  // hardTrigger (context overflow triggered this)
		nowMs,
	)
	if err != nil {
		return false, fmt.Errorf("create compaction sweep: %w", err)
	}
	defer ffi.CompactionSweepDrop(handle)

	cmdJSON, err := ffi.CompactionSweepStart(handle)
	if err != nil {
		return false, fmt.Errorf("start compaction sweep: %w", err)
	}

	// Load all messages for the sweep engine.
	allMsgs, _, err := store.Load(sessionKey, 0)
	if err != nil {
		return false, fmt.Errorf("load transcript for sweep: %w", err)
	}

	for {
		var cmd struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
			return false, fmt.Errorf("parse sweep command: %w", err)
		}

		if cmd.Type == "done" {
			logger.Info("compaction sweep completed", "session", sessionKey)
			return true, nil
		}

		response, err := handleSweepCommand(cmdJSON, allMsgs, logger)
		if err != nil {
			return false, fmt.Errorf("handle sweep command: %w", err)
		}

		respJSON, err := json.Marshal(response)
		if err != nil {
			return false, fmt.Errorf("marshal sweep response: %w", err)
		}

		cmdJSON, err = ffi.CompactionSweepStep(handle, respJSON)
		if err != nil {
			return false, fmt.Errorf("compaction sweep step: %w", err)
		}
	}
}

// handleSweepCommand processes a command from the Rust sweep engine.
func handleSweepCommand(cmdJSON json.RawMessage, msgs []ChatMessage, logger *slog.Logger) (any, error) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case "fetchCandidates":
		// Return messages eligible for compaction (all except tail).
		items := make([]map[string]any, len(msgs))
		for i, msg := range msgs {
			tokenCount := estimateTokens(msg.Content)
			items[i] = map[string]any{
				"ordinal":    i,
				"messageId":  i,
				"role":       msg.Role,
				"tokenCount": tokenCount,
				"timestamp":  msg.Timestamp,
			}
		}
		return map[string]any{
			"type":  "candidates",
			"items": items,
		}, nil

	case "summarize":
		// The sweep engine wants an LLM-generated summary.
		// For now, return a simple concatenation as placeholder.
		// In production, this would call the LLM.
		var summarizeCmd struct {
			MessageIDs []int `json:"messageIds"`
		}
		if err := json.Unmarshal(cmdJSON, &summarizeCmd); err != nil {
			logger.Warn("failed to parse summarize command", "error", err)
		}
		logger.Info("compaction sweep: summarize requested",
			"messageCount", len(summarizeCmd.MessageIDs))

		// Build a condensed summary from the referenced messages.
		var parts []string
		for _, id := range summarizeCmd.MessageIDs {
			if id >= 0 && id < len(msgs) {
				text := msgs[id].Content
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				parts = append(parts, fmt.Sprintf("[%s] %s", msgs[id].Role, text))
			}
		}
		summary := strings.Join(parts, "\n")
		return map[string]any{
			"type":    "summary",
			"text":    summary,
			"tokenCount": estimateTokens(summary),
		}, nil

	default:
		return map[string]any{"type": "empty"}, nil
	}
}

// handleContextOverflow is called when the LLM returns a context overflow error.
// It evaluates compaction, runs a sweep if needed, and rebuilds messages.
func handleContextOverflow(
	store TranscriptStore,
	sessionKey string,
	ctxCfg ContextConfig,
	compCfg CompactionConfig,
	logger *slog.Logger,
) ([]llm.Message, error) {
	// Evaluate whether compaction would help.
	// We know context overflowed, so estimate stored tokens at ~120% of budget
	// to ensure the threshold check triggers compaction.
	estimatedStored := ctxCfg.TokenBudget + ctxCfg.TokenBudget/5
	decision, err := evaluateCompaction(compCfg, estimatedStored, estimatedStored, ctxCfg.TokenBudget)
	if err != nil {
		logger.Warn("compaction evaluation failed", "error", err)
	}

	if decision != nil && decision.ShouldCompact {
		swept, err := runCompactionSweep(store, sessionKey, compCfg, ctxCfg.TokenBudget, logger)
		if err != nil {
			logger.Warn("compaction sweep failed", "error", err)
		}
		if swept {
			// Reload with reduced context.
			result, err := assembleContext(store, sessionKey, ctxCfg, logger)
			if err != nil {
				return nil, fmt.Errorf("reassemble after compaction: %w", err)
			}
			return result.Messages, nil
		}
	}

	// Fallback: halve the context window and reload.
	reducedCfg := ctxCfg
	reducedCfg.TokenBudget /= 2
	if reducedCfg.MaxMessages > 10 {
		reducedCfg.MaxMessages /= 2
	}
	result, err := assembleContext(store, sessionKey, reducedCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("reassemble with reduced budget: %w", err)
	}
	return result.Messages, nil
}
