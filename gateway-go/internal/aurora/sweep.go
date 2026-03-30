// Sweep orchestrator: host-side driver for the Rust compaction sweep state machine.
//
// Receives SweepCommand JSON from the Rust engine, executes the required I/O
// (DB reads, LLM calls, DB writes), and returns SweepResponse JSON to advance
// the state machine. This replaces the stub handlers in chat/compaction.go.
package aurora

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

const inlineFactExtractionTimeout = 2 * time.Second

// parseConversationID extracts conversation_id from a command JSON,
// accepting both snake_case (Rust FFI) and camelCase (test/direct) formats.
func parseConversationID(cmdJSON json.RawMessage) uint64 {
	var cmd struct {
		SnakeCase uint64 `json:"conversation_id"`
		CamelCase uint64 `json:"conversationId"`
	}
	json.Unmarshal(cmdJSON, &cmd)
	if cmd.SnakeCase != 0 {
		return cmd.SnakeCase
	}
	return cmd.CamelCase
}

// Summarizer is called when the sweep engine requests an LLM summary.
// The host provides an implementation that calls the active LLM provider.
type Summarizer func(text string, aggressive bool, opts *SummarizeOptions) (string, error)

// FactExtractor is called after a summary is persisted to extract important
// facts for long-term memory. This replaces the async flushMemory/transferSummary
// bridge with synchronous inline extraction during the sweep.
// Returns nil if no facts should be extracted (e.g., leaf summaries).
type FactExtractor func(summaryContent string, depth uint32) error

// SummarizeOptions passed from Rust for LLM hint tuning.
type SummarizeOptions struct {
	PreviousSummary *string `json:"previousSummary,omitempty"`
	IsCondensed     *bool   `json:"isCondensed,omitempty"`
	Depth           *uint32 `json:"depth,omitempty"`
	TargetTokens    *uint32 `json:"targetTokens,omitempty"`
}

// SweepConfig configures a compaction sweep run.
type SweepConfig struct {
	ContextThreshold       float64 `json:"contextThreshold"`
	FreshTailCount         uint32  `json:"freshTailCount"`
	LeafMinFanout          uint32  `json:"leafMinFanout"`
	CondensedMinFanout     uint32  `json:"condensedMinFanout"`
	CondensedMinFanoutHard uint32  `json:"condensedMinFanoutHard"`
	IncrementalMaxDepth    int32   `json:"incrementalMaxDepth"`
	LeafChunkTokens        *uint32 `json:"leafChunkTokens,omitempty"`
	LeafTargetTokens       uint32  `json:"leafTargetTokens"`
	CondensedTargetTokens  uint32  `json:"condensedTargetTokens"`
	MaxRounds              uint32  `json:"maxRounds"`
	Timezone               *string `json:"timezone,omitempty"`
}

// DefaultSweepConfig returns production defaults.
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		ContextThreshold:       0.75,
		FreshTailCount:         8,
		LeafMinFanout:          8,
		CondensedMinFanout:     4,
		CondensedMinFanoutHard: 2,
		IncrementalMaxDepth:    0,
		LeafTargetTokens:       600,
		CondensedTargetTokens:  900,
		MaxRounds:              10,
	}
}

// SweepResult is the parsed Done command from the sweep engine.
type SweepResult struct {
	ActionTaken      bool    `json:"actionTaken"`
	TokensBefore     uint64  `json:"tokensBefore"`
	TokensAfter      uint64  `json:"tokensAfter"`
	CreatedSummaryID *string `json:"createdSummaryId,omitempty"`
	Condensed        bool    `json:"condensed"`
	Level            *string `json:"level,omitempty"`
}

// RunSweep executes a full compaction sweep using the Rust FFI engine
// with the Aurora store providing all host-side I/O.
func RunSweep(
	store *Store,
	conversationID uint64,
	tokenBudget uint64,
	cfg SweepConfig,
	summarize Summarizer,
	force bool,
	hardTrigger bool,
	logger *slog.Logger,
	extractFacts ...FactExtractor, // optional: inline fact extraction during persist
) (*SweepResult, error) {
	if !ffi.Available {
		return nil, fmt.Errorf("aurora sweep: FFI unavailable")
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("aurora sweep: marshal config: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	handle, err := ffi.CompactionSweepNew(string(configJSON), conversationID, tokenBudget, force, hardTrigger, nowMs)
	if err != nil {
		return nil, fmt.Errorf("aurora sweep: create engine: %w", err)
	}
	defer ffi.CompactionSweepDrop(handle)

	cmdJSON, err := ffi.CompactionSweepStart(handle)
	if err != nil {
		return nil, fmt.Errorf("aurora sweep: start: %w", err)
	}

	const maxIterations = 200
	for i := 0; i < maxIterations; i++ {
		// Parse only the type field to dispatch — handlers re-parse their own fields.
		var cmd struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
			return nil, fmt.Errorf("aurora sweep: parse cmd: %w", err)
		}

		// Terminal state.
		if cmd.Type == "done" {
			var done struct {
				Result SweepResult `json:"result"`
			}
			if err := json.Unmarshal(cmdJSON, &done); err != nil {
				return nil, fmt.Errorf("aurora sweep: parse done: %w", err)
			}
			logger.Info("aurora sweep completed",
				"actionTaken", done.Result.ActionTaken,
				"tokensBefore", done.Result.TokensBefore,
				"tokensAfter", done.Result.TokensAfter,
				"condensed", done.Result.Condensed,
			)
			return &done.Result, nil
		}

		var fe FactExtractor
		if len(extractFacts) > 0 {
			fe = extractFacts[0]
		}
		resp, err := handleCommand(store, cmd.Type, cmdJSON, summarize, fe, logger)
		if err != nil {
			return nil, fmt.Errorf("aurora sweep: handle %s: %w", cmd.Type, err)
		}

		respJSON, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("aurora sweep: marshal response: %w", err)
		}

		cmdJSON, err = ffi.CompactionSweepStep(handle, respJSON)
		if err != nil {
			return nil, fmt.Errorf("aurora sweep: step: %w", err)
		}
	}

	return nil, fmt.Errorf("aurora sweep: exceeded %d iterations", maxIterations)
}

// handleCommand dispatches a SweepCommand to the appropriate store/LLM operation.
// cmdType is pre-extracted by the caller so we avoid a redundant unmarshal here.
func handleCommand(
	store *Store,
	cmdType string,
	cmdJSON json.RawMessage,
	summarize Summarizer,
	extractFacts FactExtractor,
	logger *slog.Logger,
) (any, error) {
	switch cmdType {
	case "fetchTokenCount":
		return handleFetchTokenCount(store, cmdJSON)
	case "fetchContextItems":
		return handleFetchContextItems(store, cmdJSON)
	case "fetchMessages":
		return handleFetchMessages(store, cmdJSON)
	case "fetchSummaries":
		return handleFetchSummaries(store, cmdJSON)
	case "fetchDistinctDepths":
		return handleFetchDistinctDepths(store, cmdJSON)
	case "summarize":
		return handleSummarize(cmdJSON, summarize, logger)
	case "persistLeafSummary":
		return handlePersistLeafSummary(store, cmdJSON, extractFacts, logger)
	case "persistCondensedSummary":
		return handlePersistCondensedSummary(store, cmdJSON, extractFacts, logger)
	case "persistEvent":
		return handlePersistEvent(store, cmdJSON, logger)
	default:
		if logger != nil {
			logger.Warn("aurora sweep: unknown command", "type", cmdType)
		}
		return map[string]any{"type": "persistOk"}, nil
	}
}

func handleFetchTokenCount(store *Store, cmdJSON json.RawMessage) (any, error) {
	convID := parseConversationID(cmdJSON)
	count, err := store.FetchTokenCount(convID)
	if err != nil {
		return nil, fmt.Errorf("fetch token count: %w", err)
	}
	return map[string]any{
		"type":  "tokenCount",
		"count": count,
	}, nil
}

func handleFetchContextItems(store *Store, cmdJSON json.RawMessage) (any, error) {
	convID := parseConversationID(cmdJSON)
	items, err := store.FetchContextItems(convID)
	if err != nil {
		return nil, fmt.Errorf("fetch context items: %w", err)
	}
	return map[string]any{
		"type":  "contextItems",
		"items": items,
	}, nil
}

func handleFetchMessages(store *Store, cmdJSON json.RawMessage) (any, error) {
	var cmd struct {
		MessageIDs []uint64 `json:"messageIds"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	messages, err := store.FetchMessages(cmd.MessageIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}
	return map[string]any{
		"type":     "messages",
		"messages": messages,
	}, nil
}

func handleFetchSummaries(store *Store, cmdJSON json.RawMessage) (any, error) {
	var cmd struct {
		SummaryIDs []string `json:"summaryIds"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	summaries, err := store.FetchSummaries(cmd.SummaryIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch summaries: %w", err)
	}
	return map[string]any{
		"type":      "summaries",
		"summaries": summaries,
	}, nil
}

func handleFetchDistinctDepths(store *Store, cmdJSON json.RawMessage) (any, error) {
	convID := parseConversationID(cmdJSON)
	var cmd struct {
		MaxOrdinal uint64 `json:"maxOrdinal"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	depths, err := store.FetchDistinctDepths(convID, cmd.MaxOrdinal)
	if err != nil {
		return nil, fmt.Errorf("fetch distinct depths: %w", err)
	}
	if depths == nil {
		depths = []uint32{}
	}
	return map[string]any{
		"type":   "distinctDepths",
		"depths": depths,
	}, nil
}

func handleSummarize(
	cmdJSON json.RawMessage,
	summarize Summarizer,
	logger *slog.Logger,
) (any, error) {
	var cmd struct {
		Text       string            `json:"text"`
		Aggressive bool              `json:"aggressive"`
		Options    *SummarizeOptions `json:"options,omitempty"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Debug("aurora sweep: summarize",
			"textLen", len(cmd.Text),
			"aggressive", cmd.Aggressive,
		)
	}

	text, err := summarize(cmd.Text, cmd.Aggressive, cmd.Options)
	if err != nil {
		if logger != nil {
			logger.Warn("aurora sweep: summarize failed, using fallback", "error", err)
		}
		text = ""
	}

	return map[string]any{
		"type": "summaryText",
		"text": text,
	}, nil
}

func handlePersistLeafSummary(store *Store, cmdJSON json.RawMessage, extractFacts FactExtractor, logger *slog.Logger) (any, error) {
	var cmd struct {
		Input PersistLeafInput `json:"input"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	if err := store.PersistLeafSummary(cmd.Input); err != nil {
		if logger != nil {
			logger.Error("aurora sweep: persist leaf failed", "error", err)
		}
		return map[string]any{
			"type":  "persistError",
			"error": err.Error(),
		}, nil
	}

	if logger != nil {
		logger.Debug("aurora sweep: persisted leaf summary",
			"summaryId", cmd.Input.SummaryID,
			"tokenCount", cmd.Input.TokenCount,
			"messages", len(cmd.Input.MessageIDs),
		)
	}
	return map[string]any{"type": "persistOk"}, nil
}

func handlePersistCondensedSummary(store *Store, cmdJSON json.RawMessage, extractFacts FactExtractor, logger *slog.Logger) (any, error) {
	var cmd struct {
		Input PersistCondensedInput `json:"input"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	if err := store.PersistCondensedSummary(cmd.Input); err != nil {
		if logger != nil {
			logger.Error("aurora sweep: persist condensed failed", "error", err)
		}
		return map[string]any{
			"type":  "persistError",
			"error": err.Error(),
		}, nil
	}

	if logger != nil {
		logger.Debug("aurora sweep: persisted condensed summary",
			"summaryId", cmd.Input.SummaryID,
			"depth", cmd.Input.Depth,
			"tokenCount", cmd.Input.TokenCount,
		)
	}

	// Inline fact extraction: extract important facts from the condensed
	// summary and store them in long-term memory. This replaces the async
	// transferSummaryToMemory bridge with synchronous extraction.
	// Leaf summaries (depth=0) are skipped — too granular for long-term memory.
	if extractFacts != nil && cmd.Input.Depth >= 1 && cmd.Input.Content != "" {
		go func(content string, depth uint32, summaryID string) {
			start := time.Now()
			done := make(chan error, 1)
			go func() {
				done <- extractFacts(content, depth)
			}()
			select {
			case err := <-done:
				if err != nil {
					if logger != nil {
						logger.Warn("aurora sweep: inline fact extraction failed (summary saved)",
							"summaryId", summaryID, "error", err)
					}
					return
				}
				if logger != nil {
					logger.Info("aurora sweep: inline fact extraction completed",
						"summaryId", summaryID, "depth", depth, "durationMs", time.Since(start).Milliseconds())
				}
			case <-time.After(inlineFactExtractionTimeout):
				if logger != nil {
					logger.Warn("aurora sweep: inline fact extraction timeout (summary saved)",
						"summaryId", summaryID, "depth", depth, "timeoutMs", inlineFactExtractionTimeout.Milliseconds())
				}
			}
		}(cmd.Input.Content, cmd.Input.Depth, cmd.Input.SummaryID)
	}

	return map[string]any{"type": "persistOk"}, nil
}

func handlePersistEvent(store *Store, cmdJSON json.RawMessage, logger *slog.Logger) (any, error) {
	var cmd struct {
		Input PersistEventInput `json:"input"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	if err := store.PersistEvent(cmd.Input); err != nil {
		if logger != nil {
			logger.Warn("aurora sweep: persist event failed (best-effort)", "error", err)
		}
	}

	return map[string]any{"type": "persistOk"}, nil
}

// EvaluateCompaction checks whether compaction is needed using the Rust engine.
func EvaluateCompaction(
	cfg SweepConfig,
	storedTokens, liveTokens, tokenBudget uint64,
) (bool, string, error) {
	if !ffi.Available {
		// Pure-Go fallback: simple threshold check.
		current := max(storedTokens, liveTokens)
		threshold := uint64(cfg.ContextThreshold * float64(tokenBudget))
		if current > threshold {
			return true, fmt.Sprintf("tokens %d > threshold %d", current, threshold), nil
		}
		return false, "", nil
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return false, "", err
	}

	resultJSON, err := ffi.CompactionEvaluate(string(configJSON), storedTokens, liveTokens, tokenBudget)
	if err != nil {
		return false, "", err
	}

	var decision struct {
		ShouldCompact bool   `json:"shouldCompact"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal(resultJSON, &decision); err != nil {
		return false, "", err
	}
	return decision.ShouldCompact, decision.Reason, nil
}
