package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

func agentLogsToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"run_id": map[string]any{
				"type":        "string",
				"description": "특정 런 ID로 필터 (생략 시 전체)",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "로그 타입 필터: run.start, run.prep, turn.llm, turn.tool, run.end, run.error",
				"enum":        []string{"run.start", "run.prep", "turn.llm", "turn.tool", "run.end", "run.error"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "최대 반환 수 (기본 50, 최대 500)",
				"default":     50,
				"minimum":     1,
				"maximum":     500,
			},
		},
	}
}

func toolAgentLogs(w *agentlog.Writer) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if w == nil {
			return "", fmt.Errorf("agent logs not available")
		}

		var p struct {
			RunID string `json:"run_id"`
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("agent_logs params", input, &p); err != nil {
			return "", err
		}

		sessionKey := SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "", fmt.Errorf("no session context available")
		}

		result := w.Read(agentlog.ReadOpts{
			SessionKey: sessionKey,
			RunID:      p.RunID,
			Type:       p.Type,
			Limit:      p.Limit,
		})

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal result: %w", err)
		}
		return string(out), nil
	}
}
