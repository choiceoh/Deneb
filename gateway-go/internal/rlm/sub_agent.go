package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SubAgentConfig configures a single sub-LLM run.
type SubAgentConfig struct {
	Prompt       string
	System       json.RawMessage // parent system prompt to inherit (nil = use default)
	Tools        []llm.Tool
	ToolExecutor agent.ToolExecutor
	Client       agent.LLMStreamer
	Model        string
	MaxTokens    int // max output tokens (default: 500)
	MaxTurns     int // max tool-call turns (default: 3)
	Budget       *TokenBudget
	Logger       *slog.Logger
}

// SubAgentResult holds the output of a single sub-LLM run.
type SubAgentResult struct {
	Text      string `json:"text"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
	ToolCalls int    `json:"tool_calls"`
	Error     string `json:"error,omitempty"`
}

// SubAgentTask is a single task in a batch run.
type SubAgentTask struct {
	Index  int
	Prompt string
}

// BatchConfig configures a parallel batch of sub-LLM runs.
type BatchConfig struct {
	Tasks        []SubAgentTask
	System       json.RawMessage // inherited system prompt for all sub-agents
	Tools        []llm.Tool
	ToolExecutor agent.ToolExecutor
	Client       agent.LLMStreamer
	Model        string
	MaxTokens    int
	MaxTurns     int
	Budget       *TokenBudget
	Logger       *slog.Logger
}

// RunSubAgent executes a single synchronous sub-LLM call.
// The sub-LLM gets its own independent context and can use the provided tools.
func RunSubAgent(ctx context.Context, cfg SubAgentConfig) (*SubAgentResult, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Reserve tokens before starting (pessimistic). After the call we settle
	// with actual usage via Settle, returning any surplus to the pool.
	reserved := cfg.MaxTokens
	if cfg.Budget != nil && !cfg.Budget.TryReserve(reserved) {
		return &SubAgentResult{Error: "token budget exhausted"}, nil
	}

	start := time.Now()
	cfg.Logger.Info("sub-agent starting",
		"prompt_len", len(cfg.Prompt),
		"max_tokens", cfg.MaxTokens,
		"max_turns", cfg.MaxTurns,
		"budget_remaining", budgetRemaining(cfg.Budget))

	// Inherit parent system prompt if provided; otherwise use default.
	system := cfg.System
	if system == nil {
		system = llm.SystemString(SubAgentSystemPrompt())
	}

	agentCfg := agent.AgentConfig{
		MaxTurns:  cfg.MaxTurns,
		Timeout:   2 * time.Minute,
		Model:     cfg.Model,
		System:    system,
		Tools:     cfg.Tools,
		MaxTokens: cfg.MaxTokens,
	}

	messages := []llm.Message{
		llm.NewTextMessage("user", cfg.Prompt),
	}

	result, err := agent.RunAgent(ctx, agentCfg, messages, cfg.Client, cfg.ToolExecutor, agent.StreamHooks{}, cfg.Logger, nil)
	elapsed := time.Since(start)
	if err != nil {
		// Release the full reservation on failure — no tokens were actually used.
		if cfg.Budget != nil {
			cfg.Budget.Settle(reserved, 0)
		}
		cfg.Logger.Warn("sub-agent failed",
			"error", err,
			"elapsed_ms", elapsed.Milliseconds())
		return &SubAgentResult{Error: fmt.Sprintf("sub-agent error: %v", err)}, nil
	}

	// Settle: replace pessimistic reservation with actual usage.
	totalTokens := result.Usage.InputTokens + result.Usage.OutputTokens
	if cfg.Budget != nil {
		cfg.Budget.Settle(reserved, totalTokens)
	}

	cfg.Logger.Info("sub-agent completed",
		"tokens_in", result.Usage.InputTokens,
		"tokens_out", result.Usage.OutputTokens,
		"tool_calls", result.Turns,
		"elapsed_ms", elapsed.Milliseconds(),
		"budget_remaining", budgetRemaining(cfg.Budget))

	return &SubAgentResult{
		Text:      result.AllText,
		TokensIn:  result.Usage.InputTokens,
		TokensOut: result.Usage.OutputTokens,
		ToolCalls: result.Turns,
		Error:     "",
	}, nil
}

func budgetRemaining(b *TokenBudget) int {
	if b == nil {
		return -1
	}
	return b.Remaining()
}

// maxBatchConcurrency limits how many sub-LLM calls run in parallel
// within a single batch. DGX Spark local inference supports up to 16
// concurrent batches; 12 leaves headroom for the main loop.
const maxBatchConcurrency = 12

// RunSubAgentBatch executes multiple sub-LLM calls in parallel.
// All tasks share the same tools and token budget.
// Concurrency is limited to maxBatchConcurrency.
// If any task fails with an error (not a soft error in SubAgentResult.Error),
// the remaining tasks are cancelled via context.
func RunSubAgentBatch(ctx context.Context, cfg BatchConfig) ([]SubAgentResult, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]SubAgentResult, len(cfg.Tasks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxBatchConcurrency)

	for i, task := range cfg.Tasks {
		wg.Add(1)
		go func(idx int, t SubAgentTask) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-batchCtx.Done():
				results[idx] = SubAgentResult{Error: "batch cancelled"}
				return
			}

			subCfg := SubAgentConfig{
				Prompt:       t.Prompt,
				System:       cfg.System,
				Tools:        cfg.Tools,
				ToolExecutor: cfg.ToolExecutor,
				Client:       cfg.Client,
				Model:        cfg.Model,
				MaxTokens:    cfg.MaxTokens,
				MaxTurns:     cfg.MaxTurns,
				Budget:       cfg.Budget,
				Logger:       cfg.Logger.With("sub_index", t.Index),
			}

			r, err := RunSubAgent(batchCtx, subCfg)
			if err != nil {
				results[idx] = SubAgentResult{Error: err.Error()}
				cancel() // Cancel remaining tasks on hard error.
				return
			}
			// Budget exhaustion: cancel remaining to avoid wasted calls.
			if r.Error == "token budget exhausted" {
				cancel()
			}
			results[idx] = *r
		}(i, task)
	}

	wg.Wait()
	return results, nil
}

// FormatResult formats a SubAgentResult as a compact string for tool output.
func (r *SubAgentResult) FormatResult() string {
	if r.Error != "" {
		return fmt.Sprintf("오류: %s", r.Error)
	}
	return r.Text
}

// FormatBatchResults formats batch results as a JSON string for tool output.
func FormatBatchResults(results []SubAgentResult) string {
	type resultEntry struct {
		Index  int    `json:"index"`
		Result string `json:"result"`
		Error  string `json:"error,omitempty"`
		Tokens int    `json:"tokens_used"`
	}

	entries := make([]resultEntry, len(results))
	totalTokens := 0
	for i, r := range results {
		tokens := r.TokensIn + r.TokensOut
		totalTokens += tokens
		entries[i] = resultEntry{
			Index:  i,
			Result: r.Text,
			Error:  r.Error,
			Tokens: tokens,
		}
	}

	var succeeded, failed int
	for _, e := range entries {
		if e.Error != "" {
			failed++
		} else {
			succeeded++
		}
	}

	out := struct {
		Results     []resultEntry `json:"results"`
		TotalTokens int           `json:"total_tokens"`
		Succeeded   int           `json:"succeeded"`
		Failed      int           `json:"failed"`
	}{
		Results:     entries,
		TotalTokens: totalTokens,
		Succeeded:   succeeded,
		Failed:      failed,
	}

	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("배치 결과 직렬화 실패: %v", err)
	}
	return string(b)
}
