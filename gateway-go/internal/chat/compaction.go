package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Compaction defaults.
const (
	defaultContextThreshold = 0.75
	// summarizationModel is used for compaction summaries (cost-efficient).
	summarizationModel = "claude-haiku-4-5-20251001"
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

// handleContextOverflowAurora handles context overflow using the Aurora compaction system.
// When the Aurora store is available, it runs a full hierarchical sweep via Rust FFI.
// Falls back to legacy transcript-based compaction otherwise.
func handleContextOverflowAurora(
	deps runDeps,
	params RunParams,
	llmClient *llm.Client,
	logger *slog.Logger,
) ([]llm.Message, error) {
	// Try Aurora compaction first.
	if deps.auroraStore != nil {
		logger.Info("aurora: running compaction sweep on overflow")
		sweepCfg := aurora.DefaultSweepConfig()
		sweepCfg.ContextThreshold = deps.compactionCfg.ContextThreshold
		sweepCfg.FreshTailCount = uint32(deps.compactionCfg.FreshTailCount)

		summarizer := aurora.NewLLMSummarizer(llmClient, summarizationModel)

		result, err := aurora.RunSweep(
			deps.auroraStore,
			1, // single-user conversation ID
			deps.contextCfg.TokenBudget,
			sweepCfg,
			summarizer,
			true, // force (overflow already detected)
			true, // hard trigger
			logger,
		)
		if err != nil {
			logger.Warn("aurora sweep failed, falling back", "error", err)
		} else if result != nil && result.ActionTaken {
			// Reassemble context from Aurora store.
			asmCfg := aurora.AssemblyConfig{
				TokenBudget:    deps.contextCfg.TokenBudget,
				FreshTailCount: deps.contextCfg.FreshTailCount,
				MaxMessages:    deps.contextCfg.MaxMessages,
			}
			asmResult, err := aurora.Assemble(deps.auroraStore, 1, asmCfg, logger)
			if err != nil {
				return nil, fmt.Errorf("aurora reassemble after compaction: %w", err)
			}
			return asmResult.Messages, nil
		}
	}

	// Legacy fallback: use transcript store directly.
	return handleContextOverflowLegacy(
		deps.transcript, params.SessionKey,
		deps.contextCfg, deps.compactionCfg, logger,
	)
}

// handleContextOverflowLegacy is the original overflow handler using transcript-based compaction.
func handleContextOverflowLegacy(
	store TranscriptStore,
	sessionKey string,
	ctxCfg ContextConfig,
	compCfg CompactionConfig,
	logger *slog.Logger,
) ([]llm.Message, error) {
	// Estimate stored tokens as 120% of budget — accounts for messages that
	// arrived between the last compaction and the current overflow.
	estimatedStored := ctxCfg.TokenBudget + ctxCfg.TokenBudget/5
	decision, err := evaluateCompaction(compCfg, estimatedStored, estimatedStored, ctxCfg.TokenBudget)
	if err != nil {
		logger.Warn("compaction evaluation failed", "error", err)
	}

	if decision != nil && decision.ShouldCompact {
		swept, err := runCompactionSweepLegacy(store, sessionKey, compCfg, ctxCfg.TokenBudget, logger)
		if err != nil {
			logger.Warn("compaction sweep failed", "error", err)
		}
		if swept {
			result, err := assembleContext(store, sessionKey, ctxCfg, logger)
			if err != nil {
				return nil, fmt.Errorf("reassemble after compaction: %w", err)
			}
			return result.Messages, nil
		}
	}

	// Fallback: halve the context window and message limit to fit within the
	// LLM's context. This is a last resort when compaction didn't free enough space.
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

// runCompactionSweepLegacy is the original stub-based sweep for backward compatibility.
func runCompactionSweepLegacy(
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

	var conversationID uint64 = 1
	nowMs := time.Now().UnixMilli()

	handle, err := ffi.CompactionSweepNew(
		string(configJSON), conversationID, tokenBudget,
		false, true, nowMs,
	)
	if err != nil {
		return false, fmt.Errorf("create compaction sweep: %w", err)
	}
	defer ffi.CompactionSweepDrop(handle)

	cmdJSON, err := ffi.CompactionSweepStart(handle)
	if err != nil {
		return false, fmt.Errorf("start compaction sweep: %w", err)
	}

	allMsgs, _, err := store.Load(sessionKey, 0)
	if err != nil {
		return false, fmt.Errorf("load transcript for sweep: %w", err)
	}

	// Pull-based coroutine protocol with the Rust compaction state machine:
	// 1. Rust yields a SweepCommand (e.g., FetchMessages, Summarize).
	// 2. Go executes the command and marshals a SweepResponse.
	// 3. Go feeds the response back via CompactionSweepStep, which returns the next command.
	// 4. Repeat until Rust yields a "done" command with the final result.
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

		response, err := handleSweepCommandLegacy(cmdJSON, allMsgs, logger)
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

// handleSweepCommandLegacy is the original stub handler (kept for no-Aurora fallback).
func handleSweepCommandLegacy(cmdJSON json.RawMessage, msgs []ChatMessage, logger *slog.Logger) (any, error) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case "fetchCandidates":
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
		var summarizeCmd struct {
			MessageIDs []int `json:"messageIds"`
		}
		if err := json.Unmarshal(cmdJSON, &summarizeCmd); err != nil {
			logger.Warn("failed to parse summarize command", "error", err)
		}

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
			"type":       "summary",
			"text":       summary,
			"tokenCount": estimateTokens(summary),
		}, nil

	default:
		return map[string]any{"type": "empty"}, nil
	}
}
