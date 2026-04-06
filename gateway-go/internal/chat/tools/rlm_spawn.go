package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
)

// SpawnFunc is the signature for launching a single sub-LLM call.
type SpawnFunc func(ctx context.Context, prompt string, tools []string, maxTokens, maxTurns int) (*rlm.SubAgentResult, error)

// SpawnBatchFunc is the signature for launching parallel sub-LLM calls.
type SpawnBatchFunc func(ctx context.Context, prompts []string, tools []string, maxTokens int) ([]rlm.SubAgentResult, error)

// ToolLLMSpawn returns a tool for synchronous single sub-LLM execution.
// The spawnFn closure captures the LLM client, tool executor, and available tools.
func ToolLLMSpawn(spawnFn SpawnFunc) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task      string   `json:"task"`
			Tools     []string `json:"tools"`
			MaxTokens int      `json:"max_tokens"`
			MaxTurns  int      `json:"max_turns"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.Task == "" {
			return "task는 필수입니다.", nil
		}
		if len([]rune(p.Task)) > 5000 {
			return "task가 너무 깁니다 (최대 5000자).", nil
		}
		if p.MaxTokens <= 0 {
			p.MaxTokens = 500
		}
		if p.MaxTurns <= 0 {
			p.MaxTurns = 3
		}

		result, err := spawnFn(ctx, p.Task, p.Tools, p.MaxTokens, p.MaxTurns)
		if err != nil {
			return fmt.Sprintf("서브 LLM 실행 오류: %v", err), nil
		}

		return result.FormatResult(), nil
	}
}

// ToolLLMSpawnBatch returns a tool for parallel multi-sub-LLM execution.
// maxTasks caps the number of tasks per batch (from config).
func ToolLLMSpawnBatch(batchFn SpawnBatchFunc, maxTasks int) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Tasks     []string `json:"tasks"`
			Tools     []string `json:"tools"`
			MaxTokens int      `json:"max_tokens"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if len(p.Tasks) == 0 {
			return "tasks 배열이 비어있습니다.", nil
		}
		if len(p.Tasks) > maxTasks {
			return fmt.Sprintf("tasks는 최대 %d개까지 가능합니다.", maxTasks), nil
		}
		if p.MaxTokens <= 0 {
			p.MaxTokens = 500
		}

		results, err := batchFn(ctx, p.Tasks, p.Tools, p.MaxTokens)
		if err != nil {
			return fmt.Sprintf("배치 실행 오류: %v", err), nil
		}

		return rlm.FormatBatchResults(results), nil
	}
}
