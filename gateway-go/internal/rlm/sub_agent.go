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
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 500
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Check token budget before starting.
	if cfg.Budget != nil && cfg.Budget.Remaining() < 100 {
		return &SubAgentResult{Error: "token budget exhausted"}, nil
	}

	agentCfg := agent.AgentConfig{
		MaxTurns:  cfg.MaxTurns,
		Timeout:   2 * time.Minute,
		Model:     cfg.Model,
		System:    llm.SystemString(SubAgentSystemPrompt()),
		Tools:     cfg.Tools,
		MaxTokens: cfg.MaxTokens,
	}

	messages := []llm.Message{
		llm.NewTextMessage("user", cfg.Prompt),
	}

	result, err := agent.RunAgent(ctx, agentCfg, messages, cfg.Client, cfg.ToolExecutor, agent.StreamHooks{}, cfg.Logger, nil)
	if err != nil {
		return &SubAgentResult{Error: fmt.Sprintf("sub-agent error: %v", err)}, nil
	}

	// Track token usage in shared budget.
	totalTokens := result.Usage.InputTokens + result.Usage.OutputTokens
	if cfg.Budget != nil {
		cfg.Budget.Consume(totalTokens)
	}

	return &SubAgentResult{
		Text:      result.AllText,
		TokensIn:  result.Usage.InputTokens,
		TokensOut: result.Usage.OutputTokens,
		ToolCalls: result.Turns,
		Error:     "",
	}, nil
}

// RunSubAgentBatch executes multiple sub-LLM calls in parallel.
// All tasks share the same tools and token budget.
func RunSubAgentBatch(ctx context.Context, cfg BatchConfig) ([]SubAgentResult, error) {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 500
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	results := make([]SubAgentResult, len(cfg.Tasks))
	var wg sync.WaitGroup

	for i, task := range cfg.Tasks {
		wg.Add(1)
		go func(idx int, t SubAgentTask) {
			defer wg.Done()

			subCfg := SubAgentConfig{
				Prompt:       t.Prompt,
				Tools:        cfg.Tools,
				ToolExecutor: cfg.ToolExecutor,
				Client:       cfg.Client,
				Model:        cfg.Model,
				MaxTokens:    cfg.MaxTokens,
				MaxTurns:     cfg.MaxTurns,
				Budget:       cfg.Budget,
				Logger:       cfg.Logger.With("sub_index", t.Index),
			}

			r, err := RunSubAgent(ctx, subCfg)
			if err != nil {
				results[idx] = SubAgentResult{Error: err.Error()}
				return
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

	out := struct {
		Results     []resultEntry `json:"results"`
		TotalTokens int           `json:"total_tokens"`
	}{
		Results:     entries,
		TotalTokens: totalTokens,
	}

	b, _ := json.Marshal(out)
	return string(b)
}
